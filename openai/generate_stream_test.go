// Copyright 2026 Alcova AI
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adkopenai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestModelStreamingYieldsPartialThenCompleteResponse(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		stream := strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_stream","model":"gpt-5.6-luna"}}`,
			`data: {"type":"response.output_text.delta","delta":"stream-ok"}`,
			`data: {"type":"response.completed","response":{"id":"resp_stream","model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"stream-ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"provider_metadata":{"gateway":{"generationId":"gen_stream","routing":{"resolvedProvider":"openai"}}}}}`,
			`data: [DONE]`,
			"",
		}, "\n\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})
	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL("https://example.invalid/v1"),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	)
	llm, err := newTestModel(
		testConfig{Client: client, CanonicalModel: "gpt-5.6-luna"},
		withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: false}),
	)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}

	request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("ping", genai.RoleUser)}}
	var got []*model.LLMResponse
	for response, err := range llm.GenerateContent(t.Context(), request, true) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		got = append(got, response)
	}
	if len(got) != 2 {
		t.Fatalf("stream response count = %d, want 2", len(got))
	}
	if !got[0].Partial || got[0].TurnComplete {
		t.Fatalf("partial response flags = Partial:%t TurnComplete:%t", got[0].Partial, got[0].TurnComplete)
	}
	if got[1].Partial || !got[1].TurnComplete {
		t.Fatalf("final response flags = Partial:%t TurnComplete:%t", got[1].Partial, got[1].TurnComplete)
	}
	if got[1].Content.Parts[0].Text != "stream-ok" || got[1].UsageMetadata.TotalTokenCount != 2 {
		t.Fatalf("final response = %#v", got[1])
	}
	metadata, ok := adkmodels.MetadataFromResponse(got[1])
	if !ok || metadata.GenerationID != "gen_stream" || metadata.ResolvedProvider != "openai" {
		t.Fatalf("Vercel metadata = %+v, found = %t", metadata, ok)
	}
}

func TestStreamThoughtDisplayPreservesReasoningReplay(t *testing.T) {
	for _, display := range []bool{false, true} {
		t.Run(fmt.Sprintf("display=%t", display), func(t *testing.T) {
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := strings.Join([]string{
					`data: {"type":"response.reasoning_text.delta","delta":"reasoning text"}`,
					`data: {"type":"response.reasoning_summary_text.delta","delta":"summary text"}`,
					`data: {"type":"response.output_text.delta","delta":"answer"}`,
					`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"rs_1","type":"reasoning","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"summary text"}]},{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}}`,
					"",
				}, "\n\n")
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			llm, err := newTestModel(testConfig{Client: openai.NewClient(option.WithAPIKey("test"), option.WithHTTPClient(&http.Client{Transport: transport})), CanonicalModel: "gpt-test"}, withReasoning(ReasoningConfig{Summary: shared.ReasoningSummaryAuto}))
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: display}}}
			var final *model.LLMResponse
			var thoughts, answer strings.Builder
			for r, e := range llm.GenerateContent(t.Context(), req, true) {
				if e != nil {
					t.Fatal(e)
				}
				if !r.Partial {
					final = r
					continue
				}
				for _, p := range r.Content.Parts {
					if p.Thought {
						thoughts.WriteString(p.Text)
					} else {
						answer.WriteString(p.Text)
					}
				}
			}
			if answer.String() != "answer" {
				t.Errorf("streamed answer = %q", answer.String())
			}
			if display && thoughts.String() != "reasoning textsummary text" {
				t.Errorf("streamed thoughts = %q", thoughts.String())
			}
			if !display && thoughts.Len() != 0 {
				t.Errorf("hidden thoughts were displayed: %q", thoughts.String())
			}
			if final == nil || final.Content == nil {
				t.Fatal("missing final response")
			}
			thought := final.Content.Parts[0]
			if !display && thought.Text != "" {
				t.Error("final thought text was displayed")
			}
			if display && thought.Text != "summary text" {
				t.Errorf("final summary = %q", thought.Text)
			}
			replay, err := converters.ContentsToResponseInput([]*genai.Content{final.Content})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(replay)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"encrypted_content":"opaque"`) {
				t.Errorf("reasoning replay lost: %s", raw)
			}
		})
	}
}
