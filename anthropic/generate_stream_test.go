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

package adkanthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Exact shape Vertex delivers when overload arrives after the 200 OK —
// mirrors anthropic-sdk-go's error_type_test.go fixture.
const overloadedSSE = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"

const apiErrorSSE = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"Internal server error\"}}\n\n"

const messagePrefixSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-haiku-4-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"

const successSSE = messagePrefixSSE +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

const vercelMetadataSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"zai/glm-5.3-flash\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":0},\"providerMetadata\":{\"gateway\":{\"cost\":0.01,\"generationId\":\"gen_stream\",\"routing\":{\"resolvedProvider\":\"zai\",\"originalModelId\":\"zai/glm-5.3-flash\",\"canonicalSlug\":\"zai/glm-5.3-flash\",\"modelAttemptCount\":1,\"totalProviderAttemptCount\":1}}}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

const vercelUsageSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"google/gemini-3.7-flash\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":7,\"cache_read_input_tokens\":3,\"cache_creation_input_tokens\":2,\"output_tokens\":5}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

// Overload after content has already streamed — must NOT retry.
const partialThenOverloadSSE = messagePrefixSSE +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
	overloadedSSE

// Overload after a thinking delta has streamed — the thinking branch sets the
// yielded guard too, so this must not retry either.
const thinkingThenOverloadSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-haiku-4-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"weighing options\"}}\n\n" +
	overloadedSSE

const thinkingSuccessSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"google/gemini-3.6-flash\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"weighing options\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

const thoughtOnlySuccessSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"google/gemini-3.7-flash\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"weighing options\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

// newSSEServer answers the i-th request with bodies[i] (repeating the last
// body once exhausted) and counts requests.
func newSSEServer(t *testing.T, bodies ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(requests.Add(1)) - 1
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, bodies[i])
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

// newStreamTestModel builds a model against baseURL through the real SDK
// client (so mid-stream error events decode into *anthropic.Error exactly as
// in production) and stubs retrySleep to record delays without sleeping.
func newStreamTestModel(t *testing.T, baseURL string) (*anthropicModel, *[]time.Duration) {
	t.Helper()
	client := anthropic.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(baseURL))
	llm, err := newTestModel(testConfig{
		Client:         client,
		CanonicalModel: "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m := llm.(*anthropicModel)
	sleeps := &[]time.Duration{}
	m.retrySleep = func(_ context.Context, d time.Duration) error {
		*sleeps = append(*sleeps, d)
		return nil
	}
	return m, sleeps
}

type streamPair struct {
	resp *model.LLMResponse
	err  error
}

// collect drains a streaming GenerateContent call into its yielded pairs.
func collect(ctx context.Context, m *anthropicModel) []streamPair {
	return collectRequest(ctx, m, &model.LLMRequest{})
}

func collectRequest(ctx context.Context, m *anthropicModel, req *model.LLMRequest) []streamPair {
	var pairs []streamPair
	for resp, err := range m.GenerateContent(ctx, req, true) {
		pairs = append(pairs, streamPair{resp, err})
	}
	return pairs
}

func collectIncludingThoughts(ctx context.Context, m *anthropicModel) []streamPair {
	return collectRequest(ctx, m, &model.LLMRequest{Config: &genai.GenerateContentConfig{
		ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true},
	}})
}

// sseFromPayloads frames raw stream-event payloads (as used by errors_test.go
// fixtures) into SSE wire format, deriving each event: line from the
// payload's "type" field.
func sseFromPayloads(t *testing.T, payloads []string) string {
	t.Helper()
	var b strings.Builder
	for _, p := range payloads {
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(p), &ev); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		b.WriteString("event: " + ev.Type + "\ndata: " + p + "\n\n")
	}
	return b.String()
}

func TestGenerateStream_RetriesOverloadThenSucceeds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failCount int
	}{
		{"no_overload", 0},
		{"one_overload", 1},
		{"two_overloads", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := make([]string, 0, tc.failCount+1)
			for range tc.failCount {
				bodies = append(bodies, overloadedSSE)
			}
			bodies = append(bodies, successSSE)
			srv, requests := newSSEServer(t, bodies...)
			m, sleeps := newStreamTestModel(t, srv.URL)

			pairs := collect(t.Context(), m)

			for _, p := range pairs {
				if p.err != nil {
					t.Fatalf("unexpected error after %d overload(s): %v", tc.failCount, p.err)
				}
			}
			if len(pairs) != 2 {
				t.Fatalf("len(pairs) = %d, want 2 (partial + final)", len(pairs))
			}
			if !pairs[0].resp.Partial || pairs[0].resp.Content.Parts[0].Text != "Hello" {
				t.Errorf("pairs[0] = %+v, want partial 'Hello' delta", pairs[0].resp)
			}
			if !pairs[1].resp.TurnComplete {
				t.Errorf("final response TurnComplete = false, want true")
			}
			if got := pairs[1].resp.UsageMetadata.PromptTokenCount; got != 3 {
				t.Errorf("final input tokens = %d, want 3", got)
			}
			if got := pairs[1].resp.UsageMetadata.CandidatesTokenCount; got != 2 {
				t.Errorf("final output tokens = %d, want 2", got)
			}
			if got := int(requests.Load()); got != tc.failCount+1 {
				t.Errorf("requests = %d, want %d", got, tc.failCount+1)
			}
			if len(*sleeps) != tc.failCount {
				t.Fatalf("sleeps = %d, want %d", len(*sleeps), tc.failCount)
			}
			for i, d := range *sleeps {
				base := streamRetryBaseDelay << i
				if d < base || d >= base+base/4 {
					t.Errorf("sleeps[%d] = %v, want in [%v, %v)", i, d, base, base+base/4)
				}
			}
		})
	}
}

func TestGenerateStream_PreservesVercelMetadata(t *testing.T) {
	srv, _ := newSSEServer(t, vercelMetadataSSE)
	m, _ := newStreamTestModel(t, srv.URL)
	m.vercel = &adkmodels.VercelConfig{}

	pairs := collect(t.Context(), m)
	if len(pairs) != 2 || pairs[1].err != nil {
		t.Fatalf("pairs = %+v", pairs)
	}
	metadata, ok := adkmodels.MetadataFromResponse(pairs[1].resp)
	if !ok {
		t.Fatal("missing Vercel metadata on final stream response")
	}
	if metadata.GenerationID != "gen_stream" || metadata.ResolvedProvider != "zai" {
		t.Fatalf("metadata = %+v", metadata)
	}
	responseMetadata, ok := adkmodels.MetadataFromResponse(pairs[1].resp)
	if !ok || responseMetadata.ResponseID != "msg_1" {
		t.Fatalf("Anthropic response metadata = %+v, found=%t", responseMetadata, ok)
	}
}

func TestGenerateStream_UsesCumulativeGatewayUsageFromMessageDelta(t *testing.T) {
	srv, _ := newSSEServer(t, vercelUsageSSE)
	m, _ := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2 (partial + final)", len(pairs))
	}
	final := pairs[1]
	if final.err != nil {
		t.Fatalf("final error = %v", final.err)
	}
	if got := final.resp.UsageMetadata.PromptTokenCount; got != 12 {
		t.Errorf("final input tokens = %d, want 12", got)
	}
	if got := final.resp.UsageMetadata.CachedContentTokenCount; got != 3 {
		t.Errorf("final cached input tokens = %d, want 3", got)
	}
	if got := final.resp.UsageMetadata.CandidatesTokenCount; got != 5 {
		t.Errorf("final output tokens = %d, want 5", got)
	}
	metadata, ok := adkmodels.MetadataFromResponse(final.resp)
	if !ok || metadata.CacheWriteInputTokens != 2 {
		t.Fatalf("Anthropic response metadata = %+v, found=%t", metadata, ok)
	}
}

func TestGenerateStream_KeepsLargerUsageFromMessageStart(t *testing.T) {
	stream := strings.Replace(
		vercelUsageSSE,
		`"usage":{"input_tokens":0,"output_tokens":0}`,
		`"usage":{"input_tokens":9,"cache_read_input_tokens":4,"cache_creation_input_tokens":3,"output_tokens":0}`,
		1,
	)
	srv, _ := newSSEServer(t, stream)
	m, _ := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2 (partial + final)", len(pairs))
	}
	final := pairs[1]
	if final.err != nil {
		t.Fatalf("final error = %v", final.err)
	}
	if got := final.resp.UsageMetadata.PromptTokenCount; got != 16 {
		t.Errorf("final input tokens = %d, want 16", got)
	}
	if got := final.resp.UsageMetadata.CachedContentTokenCount; got != 4 {
		t.Errorf("final cached input tokens = %d, want 4", got)
	}
}

func TestGenerateStream_ExhaustsRetriesOnPersistentOverload(t *testing.T) {
	srv, requests := newSSEServer(t, overloadedSSE)
	m, sleeps := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 1 || pairs[0].resp != nil {
		t.Fatalf("pairs = %+v, want exactly one error pair", pairs)
	}
	err := pairs[0].err
	// The wrap must stay identical to the pre-retry behaviour so caller-side
	// handling and error grouping are unchanged on exhaustion.
	if !strings.HasPrefix(err.Error(), "stream error: ") {
		t.Errorf("err = %q, want 'stream error: ' prefix", err)
	}
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) || apierr.Type() != anthropic.ErrorTypeOverloadedError {
		t.Errorf("errors.As detection of overloaded_error failed through the wrap: %v", err)
	}
	if got := int(requests.Load()); got != streamMaxAttempts {
		t.Errorf("requests = %d, want %d", got, streamMaxAttempts)
	}
	if len(*sleeps) != streamMaxAttempts-1 {
		t.Errorf("sleeps = %d, want %d", len(*sleeps), streamMaxAttempts-1)
	}
}

func TestGenerateStream_NoRetryAfterPartialOutput(t *testing.T) {
	srv, requests := newSSEServer(t, partialThenOverloadSSE)
	m, sleeps := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2 (partial then error)", len(pairs))
	}
	if pairs[0].err != nil || !pairs[0].resp.Partial || pairs[0].resp.Content.Parts[0].Text != "Hi" {
		t.Errorf("pairs[0] = %+v, want partial 'Hi' delta", pairs[0])
	}
	var apierr *anthropic.Error
	if !errors.As(pairs[1].err, &apierr) || apierr.Type() != anthropic.ErrorTypeOverloadedError {
		t.Errorf("pairs[1].err = %v, want wrapped overloaded_error", pairs[1].err)
	}
	if got := int(requests.Load()); got != 1 {
		t.Errorf("requests = %d, want 1 — an overload after yielded content must not retry", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %d, want 0", len(*sleeps))
	}
}

func TestGenerateStream_NoRetryAfterThinkingOutput(t *testing.T) {
	srv, requests := newSSEServer(t, thinkingThenOverloadSSE)
	m, sleeps := newStreamTestModel(t, srv.URL)

	pairs := collectIncludingThoughts(t.Context(), m)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2 (thinking partial then error)", len(pairs))
	}
	if pairs[0].err != nil || !pairs[0].resp.Partial || !pairs[0].resp.Content.Parts[0].Thought {
		t.Errorf("pairs[0] = %+v, want partial thinking delta", pairs[0])
	}
	var apierr *anthropic.Error
	if !errors.As(pairs[1].err, &apierr) || apierr.Type() != anthropic.ErrorTypeOverloadedError {
		t.Errorf("pairs[1].err = %v, want wrapped overloaded_error", pairs[1].err)
	}
	if got := int(requests.Load()); got != 1 {
		t.Errorf("requests = %d, want 1 — an overload after yielded thinking must not retry", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %d, want 0", len(*sleeps))
	}
}

func TestGenerateStream_RetriesAfterHiddenThinking(t *testing.T) {
	srv, requests := newSSEServer(t, thinkingThenOverloadSSE, successSSE)
	m, sleeps := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2 (text partial and final response)", len(pairs))
	}
	for _, pair := range pairs {
		if pair.err != nil {
			t.Fatalf("unexpected error: %v", pair.err)
		}
	}
	if got := int(requests.Load()); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
	if len(*sleeps) != 1 {
		t.Errorf("sleeps = %d, want 1", len(*sleeps))
	}
}

func TestGenerateStream_HonoursIncludeThoughts(t *testing.T) {
	for _, tc := range []struct {
		name            string
		includeThoughts bool
		wantPairs       int
	}{
		{name: "hidden", includeThoughts: false, wantPairs: 2},
		{name: "included", includeThoughts: true, wantPairs: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newSSEServer(t, thinkingSuccessSSE)
			m, _ := newStreamTestModel(t, srv.URL)
			req := &model.LLMRequest{Config: &genai.GenerateContentConfig{
				ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: tc.includeThoughts},
			}}

			pairs := collectRequest(t.Context(), m, req)

			if len(pairs) != tc.wantPairs {
				t.Fatalf("len(pairs) = %d, want %d", len(pairs), tc.wantPairs)
			}
			for _, pair := range pairs {
				if pair.err != nil {
					t.Fatalf("unexpected error: %v", pair.err)
				}
			}

			if tc.includeThoughts {
				if !pairs[0].resp.Partial || !pairs[0].resp.Content.Parts[0].Thought {
					t.Errorf("pairs[0] = %+v, want partial thinking delta", pairs[0].resp)
				}
			} else if pairs[0].resp.Content.Parts[0].Text != "Hello" {
				t.Errorf("pairs[0] = %+v, want first visible delta to be text", pairs[0].resp)
			}

			final := pairs[len(pairs)-1].resp
			if !final.TurnComplete {
				t.Error("final response TurnComplete = false, want true")
			}
			wantFinalParts := 1
			if tc.includeThoughts {
				wantFinalParts = 2
			}
			if len(final.Content.Parts) != wantFinalParts {
				t.Fatalf("len(final.Content.Parts) = %d, want %d", len(final.Content.Parts), wantFinalParts)
			}
		})
	}
}

func TestGenerateStream_PreservesHiddenThoughtOnlyTurn(t *testing.T) {
	srv, _ := newSSEServer(t, thoughtOnlySuccessSSE)
	m, _ := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1 final response", len(pairs))
	}
	if pairs[0].err != nil {
		t.Fatalf("unexpected error: %v", pairs[0].err)
	}
	resp := pairs[0].resp
	if resp == nil || !resp.TurnComplete || resp.Content == nil || len(resp.Content.Parts) != 1 {
		t.Fatalf("response = %+v, want completed response with one content part", resp)
	}
	if part := resp.Content.Parts[0]; !part.Thought || part.Text != "" {
		t.Errorf("part = %+v, want empty thought marker", part)
	}
}

func TestGenerateStream_NoRetryOnNonOverloadedError(t *testing.T) {
	srv, requests := newSSEServer(t, apiErrorSSE)
	m, sleeps := newStreamTestModel(t, srv.URL)

	pairs := collect(t.Context(), m)

	if len(pairs) != 1 || pairs[0].resp != nil {
		t.Fatalf("pairs = %+v, want exactly one error pair", pairs)
	}
	var apierr *anthropic.Error
	if !errors.As(pairs[0].err, &apierr) || apierr.Type() != anthropic.ErrorTypeAPIError {
		t.Errorf("err = %v, want wrapped api_error", pairs[0].err)
	}
	if got := int(requests.Load()); got != 1 {
		t.Errorf("requests = %d, want 1 — api_error must not retry", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %d, want 0", len(*sleeps))
	}
}

func TestGenerateStream_NoRetryOnDirectAPI529(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(529)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	}))
	t.Cleanup(srv.Close)
	m, sleeps := newStreamTestModel(t, srv.URL)
	// Zero out the SDK's own HTTP-level retries so the request count isolates
	// what the adapter adds on top of them.
	m.client = anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(srv.URL),
		option.WithMaxRetries(0),
	)

	pairs := collect(t.Context(), m)

	if len(pairs) != 1 || pairs[0].resp != nil {
		t.Fatalf("pairs = %+v, want exactly one error pair", pairs)
	}
	var apierr *anthropic.Error
	if !errors.As(pairs[0].err, &apierr) || apierr.Type() != anthropic.ErrorTypeOverloadedError {
		t.Errorf("err = %v, want wrapped overloaded_error", pairs[0].err)
	}
	if got := int(requests.Load()); got != 1 {
		t.Errorf("requests = %d, want 1 — a direct-API 529 already spent the SDK's retries and must not be retried again", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %d, want 0", len(*sleeps))
	}
}

func TestGenerateStream_AbortsWhenBackoffCancelled(t *testing.T) {
	srv, requests := newSSEServer(t, overloadedSSE)
	m, _ := newStreamTestModel(t, srv.URL)
	m.retrySleep = func(context.Context, time.Duration) error {
		return context.Canceled
	}

	pairs := collect(t.Context(), m)

	if len(pairs) != 1 || pairs[0].resp != nil {
		t.Fatalf("pairs = %+v, want exactly one error pair", pairs)
	}
	// Both identities must survive the wrap: the overload for detection, the
	// cancellation for callers that filter caller-initiated aborts.
	var apierr *anthropic.Error
	if !errors.As(pairs[0].err, &apierr) || apierr.Type() != anthropic.ErrorTypeOverloadedError {
		t.Errorf("err = %v, want the overload detectable via errors.As", pairs[0].err)
	}
	if !errors.Is(pairs[0].err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled detectable via errors.Is", pairs[0].err)
	}
	if got := int(requests.Load()); got != 1 {
		t.Errorf("requests = %d, want 1 — no attempt after a cancelled backoff", got)
	}
}

func TestGenerateStream_InterruptedOutputIsNotRetried(t *testing.T) {
	srv, requests := newSSEServer(t, sseFromPayloads(t, interruptedToolCallStream))
	m, sleeps := newStreamTestModel(t, srv.URL)

	pairs := collectIncludingThoughts(t.Context(), m)

	// The fixture streams a thinking delta and a text delta before the
	// truncated tool call, so those arrive as partials ahead of the error.
	if len(pairs) != 3 {
		t.Fatalf("len(pairs) = %d, want 3 (thinking, text, interruption)", len(pairs))
	}
	for _, p := range pairs[:2] {
		if p.err != nil || !p.resp.Partial {
			t.Fatalf("pair = %+v, want a partial delta", p)
		}
	}
	var interrupted *OutputInterruptedError
	if !errors.As(pairs[2].err, &interrupted) {
		t.Fatalf("err = %v (%T), want *OutputInterruptedError", pairs[2].err, pairs[2].err)
	}
	if got := int(requests.Load()); got != 1 {
		t.Errorf("requests = %d, want 1 — interruptions must never retry", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %d, want 0", len(*sleeps))
	}
}

func TestGenerateStream_IncompleteToolBlockStop(t *testing.T) {
	payloads := append([]string(nil), interruptedToolCallStream[:len(interruptedToolCallStream)-2]...)
	payloads = append(payloads, `{"type":"content_block_stop","index":3}`)
	payloads = append(payloads, interruptedToolCallStream[len(interruptedToolCallStream)-2:]...)
	srv, requests := newSSEServer(t, sseFromPayloads(t, payloads))
	m, sleeps := newStreamTestModel(t, srv.URL)
	pairs := collectIncludingThoughts(t.Context(), m)
	if len(pairs) != 3 {
		t.Fatalf("got %d responses, want two partials and an interruption", len(pairs))
	}
	var interrupted *OutputInterruptedError
	if !errors.As(pairs[2].err, &interrupted) {
		t.Fatalf("got %v, want an interruption", pairs[2].err)
	}
	if interrupted.ToolName != "save_file" || interrupted.ToolID != "toolu_cut" || interrupted.PartialInput != `{"path": "/reports/summ` {
		t.Fatalf("truncated tool details lost: %+v", interrupted)
	}
	if interrupted.StopReason != anthropic.StopReasonMaxTokens {
		t.Fatalf("stop reason = %q, want max_tokens", interrupted.StopReason)
	}
	message, err := accumulateEvents(t, payloads)
	if err == nil || message.StopReason != anthropic.StopReasonMaxTokens || message.Usage.OutputTokens != 50 {
		t.Fatalf("final metadata lost: stop=%q output=%d err=%v", message.StopReason, message.Usage.OutputTokens, err)
	}
	for _, part := range interrupted.Parts {
		if part.FunctionCall != nil && part.FunctionCall.Name == "save_file" {
			t.Fatal("incomplete tool call must not be returned as executable content")
		}
	}
	if requests.Load() != 1 || len(*sleeps) != 0 {
		t.Fatal("incomplete tool output must not be retried")
	}
}

func TestGenerateStream_IncompleteToolBlockConnectionEnds(t *testing.T) {
	for _, overload := range []bool{false, true} {
		t.Run(fmt.Sprintf("overload-%t", overload), func(t *testing.T) {
			// No text delta: a transport error must still not trigger a retry.
			payloads := []string{
				interruptedToolCallStream[0],
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"cut","name":"save_file","input":{}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
				`{"type":"content_block_stop","index":0}`,
			}
			if overload {
				payloads = append(payloads, `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`)
			}
			srv, requests := newSSEServer(t, sseFromPayloads(t, payloads))
			m, sleeps := newStreamTestModel(t, srv.URL)
			pairs := collectIncludingThoughts(t.Context(), m)
			if len(pairs) != 1 {
				t.Fatalf("got %d pairs, want one interruption", len(pairs))
			}
			var interrupted *OutputInterruptedError
			if !errors.As(pairs[0].err, &interrupted) || interrupted.PartialInput != `{"path":` || interrupted.ToolID != "cut" {
				t.Fatalf("interruption details lost: %v", pairs[0].err)
			}
			if overload {
				var apiErr *anthropic.Error
				if !errors.As(interrupted, &apiErr) {
					t.Fatalf("underlying error lost: %v", interrupted)
				}
			}
			if requests.Load() != 1 || len(*sleeps) != 0 {
				t.Fatal("incomplete tool must not retry")
			}
		})
	}
}

func TestSleepWithContext(t *testing.T) {
	t.Run("cancel_aborts_promptly", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		start := time.Now()
		err := sleepWithContext(ctx, 5*time.Second)
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Errorf("sleepWithContext took %v, want prompt abort", elapsed)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("elapses_normally", func(t *testing.T) {
		if err := sleepWithContext(t.Context(), 5*time.Millisecond); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
}

// The success and token-limit streams were captured from Haiku 5.5 through
// Vercel Messages on 8 October 2026. IDs are normalised and gateway metadata
// removed. The damaged cases below are controlled changes to captured bytes,
// not claims that these modified streams were returned by the provider.
func TestGenerateStream_CapturedHaikuOutputFailures(t *testing.T) {
	limited, err := os.ReadFile("testdata/haiku55-token-limit.sse")
	if err != nil {
		t.Fatal(err)
	}
	successful, err := os.ReadFile("testdata/haiku55-xhigh-success.sse")
	if err != nil {
		t.Fatal(err)
	}
	damaged := strings.Replace(string(successful), `"partial_json":"\"}"`, `"partial_json":"\"}}"`, 1)
	if damaged == string(successful) {
		t.Fatal("fixture mutation missed tool input")
	}
	prefix := string(limited)
	cut := strings.Index(prefix, "event: content_block_stop")
	if cut < 0 {
		cut = strings.Index(prefix, "event: message_delta")
	}
	if cut < 0 {
		t.Fatal("captured stream has no terminal event")
	}
	for _, tc := range []struct {
		name, body, want string
		success          bool
	}{
		{"real XHigh success", string(successful), "", true},
		{"real max_tokens", string(limited), "reason=max_tokens", false},
		{"malformed finished input", damaged, "reason=invalid_tool_input", false},
		{"connection closes mid input", prefix[:cut], "reason=stream_interrupted", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, requests := newSSEServer(t, tc.body)
			m, sleeps := newStreamTestModel(t, srv.URL)
			pairs := collect(t.Context(), m)
			var final *model.LLMResponse
			var failure error
			for _, pair := range pairs {
				if pair.err != nil {
					failure = pair.err
				}
				if pair.resp != nil && !pair.resp.Partial {
					final = pair.resp
				}
			}
			if tc.success {
				if failure != nil || final == nil || final.Content == nil {
					t.Fatalf("successful provider tool call lost: %v", failure)
				}
				var got *genai.FunctionCall
				for _, part := range final.Content.Parts {
					if part.FunctionCall != nil {
						got = part.FunctionCall
					}
				}
				if got == nil || got.Name != "write_file" || got.Args["content"] != "Hello" {
					t.Fatalf("tool call changed: %+v", got)
				}
			} else {
				var output *OutputInterruptedError
				if !errors.As(failure, &output) || !strings.Contains(failure.Error(), tc.want) {
					t.Fatalf("wrong failure classification: %v", failure)
				}
				if final != nil {
					t.Fatal("invalid input became an executable response")
				}
				if tc.name == "malformed finished input" {
					var syntaxErr *json.SyntaxError
					if !errors.As(failure, &syntaxErr) || syntaxErr.Offset == 0 {
						t.Fatalf("JSON syntax cause/position lost: %v", failure)
					}
					if strings.Contains(failure.Error(), "truncated") {
						t.Fatalf("malformed input reported as truncation: %v", failure)
					}
				}
				if tc.name == "connection closes mid input" && !errors.Is(failure, io.ErrUnexpectedEOF) {
					t.Fatalf("stream closure cause lost: %v", failure)
				}
			}
			if requests.Load() != 1 || len(*sleeps) != 0 {
				t.Fatal("adapter must leave unusable-output recovery to its caller")
			}
		})
	}
}

// Run with ADK_MODELS_LIVE=1 and AI_GATEWAY_API_KEY. The low output limit
// deliberately exercises the provider's real max_tokens termination.
func TestHaikuMessagesOutputLive(t *testing.T) {
	key := os.Getenv("AI_GATEWAY_API_KEY")
	if os.Getenv("ADK_MODELS_LIVE") != "1" || key == "" {
		t.Skip("explicit live integration settings are not set")
	}
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("xhigh/stream=%t", stream), func(t *testing.T) {
			maxTokens := int32(64000)
			if !stream {
				maxTokens = 1024
			} // The SDK requires streaming for a 64k output allowance.
			client := anthropic.NewClient(option.WithAPIKey(key), option.WithBaseURL("https://ai-gateway.vercel.sh"), option.WithMaxRetries(0))
			llm, err := NewModel(Config{Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "claude-haiku-5-5", RequestModel: "anthropic/claude-haiku-5.5", DefaultMaxOutputTokens: maxTokens, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: adkmodels.ThinkingLevelXHigh}, Vercel: &adkmodels.VercelConfig{Only: []string{"anthropic"}, ZeroDataRetention: true}}})
			if err != nil {
				t.Fatal(err)
			}
			req := haikuOutputRequest("Use write_file to save outputs/report.txt with content Hello.")
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			var call *genai.FunctionCall
			for response, err := range llm.GenerateContent(ctx, req, stream) {
				if err != nil {
					t.Fatal(err)
				}
				if response == nil || response.Partial || response.Content == nil {
					continue
				}
				for _, part := range response.Content.Parts {
					if part.FunctionCall != nil {
						call = part.FunctionCall
					}
				}
			}
			if call == nil || call.Name != "write_file" || call.Args["content"] != "Hello" {
				t.Fatalf("tool call missing/changed: %+v", call)
			}
		})
	}
	t.Run("actual token exhaustion", func(t *testing.T) {
		client := anthropic.NewClient(option.WithAPIKey(key), option.WithBaseURL("https://ai-gateway.vercel.sh"), option.WithMaxRetries(0))
		llm, err := NewModel(Config{Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "claude-haiku-5-5", RequestModel: "anthropic/claude-haiku-5.5", DefaultMaxOutputTokens: 32, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelMinimal}, Vercel: &adkmodels.VercelConfig{Only: []string{"anthropic"}, ZeroDataRetention: true}}})
		if err != nil {
			t.Fatal(err)
		}
		req := haikuOutputRequest("Use write_file now to save outputs/report.txt containing a 1000-word essay on the history of sailing.")
		req.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"write_file"}}}
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		defer cancel()
		var failure error
		for response, err := range llm.GenerateContent(ctx, req, true) {
			if err != nil {
				failure = err
			}
			if response != nil && !response.Partial {
				t.Fatal("partial tool call became executable")
			}
		}
		var output *OutputInterruptedError
		if !errors.As(failure, &output) || output.StopReason != anthropic.StopReasonMaxTokens || !strings.Contains(output.Error(), "reason=max_tokens") || output.PartialInput == "" {
			t.Fatalf("provider token-limit evidence missing: %v", failure)
		}
		t.Logf("provider stop=%s, tool=%s, input_bytes=%d, cause=%v", output.StopReason, output.ToolName, len(output.PartialInput), output.Cause)
	})
}

func haikuOutputRequest(prompt string) *model.LLMRequest {
	return &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "write_file", Description: "Save the requested report.", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"path", "content"}, "additionalProperties": false}}}}}}}
}
