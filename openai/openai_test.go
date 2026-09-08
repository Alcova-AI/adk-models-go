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
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

func TestModelUsesCallerClientAndOptionalVercelOptions(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.URL.Path, "/v1/responses"; got != want {
			t.Errorf("request path = %q, want %q", got, want)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got, want := body["model"], "openai/gpt-5.6-luna"; got != want {
			t.Errorf("wire model = %v, want %v", got, want)
		}
		if got := body["store"]; got != false {
			t.Errorf("store = %v, want false for Vercel ZDR route", got)
		}
		providerOptions, ok := body["providerOptions"].(map[string]any)
		if !ok {
			t.Fatalf("providerOptions missing: %#v", body)
		}
		gateway, ok := providerOptions["gateway"].(map[string]any)
		if !ok || gateway["zeroDataRetention"] != true {
			t.Errorf("gateway options = %#v, want zeroDataRetention=true", gateway)
		}
		if gateway["caching"] != "auto" {
			t.Errorf("gateway caching = %v, want auto", gateway["caching"])
		}
		openAI, ok := providerOptions["openai"].(map[string]any)
		if !ok || openAI["reasoningEffort"] != "low" || openAI["reasoningSummary"] != "auto" || openAI["store"] != false {
			t.Errorf("OpenAI options = %#v", openAI)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"resp_test","model":"openai/gpt-5.6-luna","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)),
			Request: r,
		}, nil
	})

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL("https://example.invalid/v1"),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	)
	llm, err := newTestModel(testConfig{
		Client:         client,
		CanonicalModel: shared.ResponsesModel("gpt-5.6-luna"),
		RequestModel:   shared.ResponsesModel("openai/gpt-5.6-luna"),
	},
		withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: true, Caching: adkmodels.GatewayCachingAuto}),
	)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}
	if got, want := llm.Name(), "gpt-5.6-luna"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("ping", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{
			ThinkingLevel:   genai.ThinkingLevelLow,
			IncludeThoughts: true,
		}},
	}
	var got string
	for response, err := range llm.GenerateContent(t.Context(), req, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		for _, part := range response.Content.Parts {
			got += part.Text
		}
	}
	if got != "ok" {
		t.Fatalf("response text = %q, want ok", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestBuildParamsSupportsFilesAndExplicitBreakpoints(t *testing.T) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("stable history", genai.RoleUser),
			genai.NewContentFromText("prior answer", genai.RoleModel),
			{
				Role: string(genai.RoleUser),
				Parts: []*genai.Part{
					{Text: "read these"},
					{InlineData: &genai.Blob{Data: []byte("pdf"), MIMEType: "application/pdf", DisplayName: "brief.pdf"}},
					{FileData: &genai.FileData{FileURI: "https://example.com/chart.png", MIMEType: "image/png"}},
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("stable instructions", "system"),
		},
	}
	adapter := &openAIModel{
		requestModel: "gpt-5.6-luna",
		promptCaching: PromptCachingConfig{
			Mode:                PromptCacheExplicit,
			Key:                 "tenant-agent",
			SystemInstruction:   &CacheBreakpoint{},
			ConversationHistory: &CacheBreakpoint{},
		},
	}
	params, err := adapter.convertRequest(req)
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	jsonText := string(raw)
	for _, fragment := range []string{
		`"prompt_cache_options":{"mode":"explicit","ttl":"30m"}`,
		`"prompt_cache_key":"tenant-agent"`,
		`"type":"input_file"`,
		`"filename":"brief.pdf"`,
		`"type":"input_image"`,
	} {
		if !strings.Contains(jsonText, fragment) {
			t.Errorf("request JSON missing %s: %s", fragment, jsonText)
		}
	}
	if got := strings.Count(jsonText, `"prompt_cache_breakpoint":{"mode":"explicit"}`); got != 2 {
		t.Errorf("explicit breakpoint count = %d, want 2: %s", got, jsonText)
	}
	if !strings.Contains(jsonText, `"text":"stable history","prompt_cache_breakpoint":{"mode":"explicit"}`) {
		t.Errorf("conversation breakpoint is not on the stable user history: %s", jsonText)
	}
	if strings.Contains(jsonText, `"text":"prior answer","prompt_cache_breakpoint"`) {
		t.Errorf("conversation breakpoint must not be on assistant output: %s", jsonText)
	}
}

func TestBuildParamsConversationBreakpointSupportsFirstTurn(t *testing.T) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("first turn", genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("stable instructions", "system"),
		},
	}
	adapter := &openAIModel{
		requestModel: "gpt-5.6-luna",
		promptCaching: PromptCachingConfig{
			Mode:                PromptCacheExplicit,
			SystemInstruction:   &CacheBreakpoint{},
			ConversationHistory: &CacheBreakpoint{},
		},
	}

	params, err := adapter.convertRequest(req)
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	jsonText := string(raw)
	if got := strings.Count(jsonText, `"prompt_cache_breakpoint":{"mode":"explicit"}`); got != 2 {
		t.Errorf("explicit breakpoint count = %d, want instruction and user breakpoints: %s", got, jsonText)
	}
	if !strings.Contains(jsonText, `"text":"first turn","prompt_cache_breakpoint"`) {
		t.Errorf("first-turn input is missing its rolling conversation breakpoint: %s", jsonText)
	}
}

func TestBuildParamsConversationBreakpointIgnoresTrailingRuntimeContext(t *testing.T) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("stable file turn", genai.RoleUser),
			genai.NewContentFromText("prior answer", genai.RoleModel),
			genai.NewContentFromText("changed final message", genai.RoleUser),
			{
				Role: string(genai.RoleUser),
				Parts: []*genai.Part{
					{Text: "<runtime_file_context>dynamic context</runtime_file_context>"},
					{InlineData: &genai.Blob{Data: []byte("image"), MIMEType: "image/png"}},
				},
			},
		},
	}
	adapter := &openAIModel{
		requestModel: "gpt-5.6-luna",
		promptCaching: PromptCachingConfig{
			Mode:                PromptCacheExplicit,
			ConversationHistory: &CacheBreakpoint{},
		},
	}

	params, err := adapter.convertRequest(req)
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	jsonText := string(raw)
	for _, stable := range []string{"stable file turn", "changed final message"} {
		if !strings.Contains(jsonText, `"text":"`+stable+`","prompt_cache_breakpoint":{"mode":"explicit"}`) {
			t.Errorf("conversation breakpoint is not on stable input %q: %s", stable, jsonText)
		}
	}
	if strings.Contains(jsonText, `"text":"<runtime_file_context>dynamic context</runtime_file_context>","prompt_cache_breakpoint"`) {
		t.Errorf("conversation breakpoint must not be on hydrated runtime files: %s", jsonText)
	}
}

func TestBuildParamsRetainsRollingConversationBreakpoints(t *testing.T) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("message A", genai.RoleUser),
			genai.NewContentFromText("response B", genai.RoleModel),
			genai.NewContentFromText("message C with a file reference", genai.RoleUser),
			genai.NewContentFromText("response D", genai.RoleModel),
			genai.NewContentFromText("message E", genai.RoleUser),
			{
				Role: string(genai.RoleUser),
				Parts: []*genai.Part{
					{Text: "hydrated files"},
					{InlineData: &genai.Blob{Data: []byte("pdf"), MIMEType: "application/pdf"}},
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("stable instructions", "system"),
		},
	}
	adapter := &openAIModel{
		requestModel: "gpt-5.6-luna",
		promptCaching: PromptCachingConfig{
			Mode:                PromptCacheExplicit,
			SystemInstruction:   &CacheBreakpoint{},
			ConversationHistory: &CacheBreakpoint{},
		},
	}

	params, err := adapter.convertRequest(req)
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	jsonText := string(raw)
	if got := strings.Count(jsonText, `"prompt_cache_breakpoint":{"mode":"explicit"}`); got != 4 {
		t.Errorf("explicit breakpoint count = %d, want 4: %s", got, jsonText)
	}
	for _, stable := range []string{"message A", "message C with a file reference", "message E"} {
		if !strings.Contains(jsonText, `"text":"`+stable+`","prompt_cache_breakpoint":{"mode":"explicit"}`) {
			t.Errorf("rolling breakpoint missing from %q: %s", stable, jsonText)
		}
	}
	for _, uncached := range []string{"response B", "response D", "hydrated files"} {
		if strings.Contains(jsonText, `"text":"`+uncached+`","prompt_cache_breakpoint"`) {
			t.Errorf("unexpected breakpoint on %q: %s", uncached, jsonText)
		}
	}
}

func TestBuildParamsStatelessResponses(t *testing.T) {
	adapter := &openAIModel{
		requestModel: "gpt-5.6-luna",
	}
	params, err := adapter.convertRequest(&model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)},
	})
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	jsonText := string(raw)
	if !strings.Contains(jsonText, `"store":false`) {
		t.Fatalf("stateless request missing store false: %s", jsonText)
	}
	if !strings.Contains(jsonText, `"reasoning.encrypted_content"`) {
		t.Fatalf("stateless request missing encrypted reasoning include: %s", jsonText)
	}
}

func TestReasoningStateRoundTripWithoutDisplayingThoughts(t *testing.T) {
	raw := `{"id":"resp_reasoning","model":"gpt-5.6-luna","status":"completed","output":[{"id":"rs_1","type":"reasoning","status":"completed","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"short summary"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	var response responses.Response
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	converted, err := converters.ResponseToLLMResponse(&response, false)
	if err != nil {
		t.Fatalf("ResponseToLLMResponse() error = %v", err)
	}
	part := converted.Content.Parts[0]
	if part.Text != "" || !part.Thought || len(part.ThoughtSignature) == 0 {
		t.Fatalf("hidden reasoning state = %#v", part)
	}

	input, err := converters.ContentsToResponseInput([]*genai.Content{{Role: string(genai.RoleModel), Parts: []*genai.Part{part}}})
	if err != nil {
		t.Fatalf("convertContents() error = %v", err)
	}
	if len(input) != 1 || input[0].OfReasoning == nil {
		t.Fatalf("replayed input = %#v, want one reasoning item", input)
	}
	encoded, err := json.Marshal(input[0].OfReasoning)
	if err != nil {
		t.Fatalf("marshal replayed reasoning: %v", err)
	}
	if !strings.Contains(string(encoded), `"encrypted_content":"opaque"`) {
		t.Fatalf("encrypted reasoning state was not preserved: %s", encoded)
	}
}

func TestNewModelValidation(t *testing.T) {
	client := openai.NewClient(option.WithAPIKey("test"))
	if _, err := newTestModel(testConfig{Client: client}); err == nil || !strings.Contains(err.Error(), "canonical model") {
		t.Fatalf("missing canonical model error = %v", err)
	}
	if _, err := newTestModel(testConfig{CanonicalModel: "gpt-5.6-luna"}); err == nil || !strings.Contains(err.Error(), "openai.NewClient") {
		t.Fatalf("zero client error = %v", err)
	}
}

func TestXHighReasoningReachesWire(t *testing.T) {
	for _, useGateway := range []bool{false, true} {
		for _, requestOverride := range []bool{false, true} {
			name := fmt.Sprintf("gateway=%t/request-override=%t", useGateway, requestOverride)
			t.Run(name, func(t *testing.T) {
				called := false
				transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					called = true
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("decode request: %v", err)
					}
					if useGateway {
						options := body["providerOptions"].(map[string]any)
						openAI := options["openai"].(map[string]any)
						if openAI["reasoningEffort"] != "xhigh" {
							t.Errorf("gateway reasoning effort = %v, want xhigh", openAI["reasoningEffort"])
						}
					} else {
						reasoning := body["reasoning"].(map[string]any)
						if reasoning["effort"] != "xhigh" {
							t.Errorf("reasoning effort = %v, want xhigh", reasoning["effort"])
						}
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"id":"resp_test","model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
						Request:    r,
					}, nil
				})
				level := genai.ThinkingLevel("XHIGH")
				request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("ping", genai.RoleUser)}}
				if requestOverride {
					request.Config = &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: level}}
					level = genai.ThinkingLevelLow
				}
				options := []testOption{withReasoning(ReasoningConfig{DefaultLevel: level})}
				if useGateway {
					options = append(options, withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: true}))
				}
				client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL("https://example.invalid/v1"), option.WithHTTPClient(&http.Client{Transport: transport}))
				llm, err := newTestModel(testConfig{Client: client, CanonicalModel: "gpt-5.6-luna"}, options...)
				if err != nil {
					t.Fatalf("newTestModel() error = %v", err)
				}
				for _, err := range llm.GenerateContent(t.Context(), request, false) {
					if err != nil {
						t.Fatalf("GenerateContent() error = %v", err)
					}
				}
				if !called {
					t.Fatal("request did not reach the transport")
				}
			})
		}
	}
}

func TestSummaryStyleRequiresIncludeThoughts(t *testing.T) {
	for _, style := range []shared.ReasoningSummary{"", shared.ReasoningSummaryAuto, shared.ReasoningSummaryConcise, shared.ReasoningSummaryDetailed} {
		for _, display := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/display=%t", style, display), func(t *testing.T) {
				adapter := &openAIModel{requestModel: "test", reasoning: reasoningConfig{DefaultLevel: genai.ThinkingLevelHigh, OpenAI: adkmodels.OpenAIReasoningConfig{Summary: style}}}
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: display}}}
				params, err := adapter.convertRequest(req)
				if err != nil {
					t.Fatal(err)
				}
				want := style
				if !display {
					want = ""
				} else if want == "" {
					want = shared.ReasoningSummaryAuto
				}
				if params.Reasoning.Summary != want {
					t.Errorf("summary = %q, want %q", params.Reasoning.Summary, want)
				}
				if params.Reasoning.Effort != shared.ReasoningEffortHigh {
					t.Errorf("reasoning effort changed: %s", params.Reasoning.Effort)
				}
			})
		}
	}
}
