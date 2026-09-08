// Copyright 2025 Alcova AI
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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/gateway"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestNewModel_RequiresConstructedClientAndCanonicalModel(t *testing.T) {
	tests := []struct {
		name string
		cfg  testConfig
		want string
	}{
		{name: "missing client", cfg: testConfig{CanonicalModel: "claude-sonnet-5"}, want: "client must be constructed"},
		{name: "missing model", cfg: testConfig{Client: testClient()}, want: "unrecognised canonical model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestModel(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("newTestModel() error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestNewModel_UsesCanonicalAndRequestModels(t *testing.T) {
	llm, err := newTestModel(testConfig{
		Client:         testClient(),
		CanonicalModel: "claude-sonnet-5",
		RequestModel:   "anthropic/claude-sonnet-5",
	}, withDefaultMaxTokens(2048))
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}
	if got := llm.Name(); got != "claude-sonnet-5" {
		t.Fatalf("Name() = %q, want claude-sonnet-5", got)
	}
	m := llm.(*anthropicModel)
	if m.wireModel() != "anthropic/claude-sonnet-5" {
		t.Fatalf("wireModel() = %q", m.wireModel())
	}
	if m.defaultMaxTokens != 2048 {
		t.Fatalf("defaultMaxTokens = %d, want 2048", m.defaultMaxTokens)
	}
}

func TestNewModel_DefaultMaxTokensSupportsNonStreaming(t *testing.T) {
	m := mustTestModel(t, "claude-haiku-4-5")
	if _, err := anthropic.CalculateNonStreamingTimeout(m.defaultMaxTokens, "claude-haiku-4-5", nil); err != nil {
		t.Fatalf("default max_tokens %d is incompatible with non-streaming requests: %v", m.defaultMaxTokens, err)
	}
}

func TestReasoningLevels(t *testing.T) {
	tests := []struct {
		name         string
		canonical    string
		config       ReasoningConfig
		request      *genai.ThinkingConfig
		wantAdaptive bool
		wantEffort   anthropic.OutputConfigEffort
		wantDisplay  string
	}{
		{
			name:         "request level selects adaptive thinking",
			wantAdaptive: true, wantEffort: anthropic.OutputConfigEffortHigh,
			config:  ReasoningConfig{},
			request: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh},
		},
		{
			name:         "adaptive uses request effort",
			config:       ReasoningConfig{DefaultLevel: genai.ThinkingLevelMedium},
			request:      &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow, IncludeThoughts: true},
			wantAdaptive: true,
			wantEffort:   anthropic.OutputConfigEffortLow,
			wantDisplay:  "summarized",
		},
		{
			name:         "adaptive uses route default",
			config:       ReasoningConfig{DefaultLevel: genai.ThinkingLevelMedium},
			wantAdaptive: true,
			wantEffort:   anthropic.OutputConfigEffortMedium,
			wantDisplay:  "omitted",
		},
		{
			name:       "minimal disables adaptive",
			wantEffort: anthropic.OutputConfigEffortLow,
			config:     ReasoningConfig{DefaultLevel: genai.ThinkingLevelHigh},
			request:    &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal},
		},
		{
			name:      "other families emit no Anthropic reasoning",
			canonical: "gpt-test",
			config:    ReasoningConfig{DefaultLevel: genai.ThinkingLevelHigh},
			request:   &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow, IncludeThoughts: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := []testOption{withReasoning(tt.config)}
			if tt.canonical != "" {
				options = append(options, withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: true}))
			}
			canonical := "claude-test"
			if tt.canonical != "" {
				canonical = tt.canonical
			}
			m := mustTestModel(t, canonical, options...)
			params, err := m.convertRequest(testRequest(tt.request))
			if err != nil {
				t.Fatalf("convertRequest() error = %v", err)
			}
			if got := params.Thinking.OfAdaptive != nil; got != tt.wantAdaptive {
				t.Fatalf("adaptive = %v, want %v", got, tt.wantAdaptive)
			}
			if params.Thinking.OfEnabled != nil {
				t.Fatalf("unexpected enabled thinking = %+v", params.Thinking.OfEnabled)
			}
			if params.OutputConfig.Effort != tt.wantEffort {
				t.Fatalf("effort = %q, want %q", params.OutputConfig.Effort, tt.wantEffort)
			}
			if tt.wantDisplay != "" {
				raw, err := json.Marshal(params.Thinking)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(raw), `"display":"`+tt.wantDisplay+`"`) {
					t.Fatalf("thinking = %s, want display %s", raw, tt.wantDisplay)
				}
			}
		})
	}
}

func TestReasoningRejectsExplicitBudget(t *testing.T) {
	m := mustTestModel(t, "claude-test", withReasoning(ReasoningConfig{}))
	budget := int32(2048)
	_, err := m.convertRequest(testRequest(&genai.ThinkingConfig{ThinkingBudget: &budget}))
	if err == nil || !strings.Contains(err.Error(), "ThinkingBudget is not supported") {
		t.Fatalf("convertRequest() error = %v", err)
	}
}

func TestCrossFamilyRequiresVercel(t *testing.T) {
	for _, canonical := range []string{"gpt-test", "gemini-test", "glm-test"} {
		_, err := NewModel(Config{Client: testClient(), Model: adkmodels.ModelConfig{CanonicalModel: canonical}})
		if err == nil {
			t.Errorf("direct %s accepted", canonical)
		}
		_, err = NewModel(Config{Client: testClient(), Model: adkmodels.ModelConfig{CanonicalModel: canonical, Vercel: &adkmodels.VercelConfig{}}})
		if err != nil {
			t.Errorf("gateway %s: %v", canonical, err)
		}
	}
}

func TestForcedToolUseDropsThinkingAndEffort(t *testing.T) {
	m := mustTestModel(t, "claude-sonnet-5", withReasoning(ReasoningConfig{
		DefaultLevel: genai.ThinkingLevelHigh,
	}))
	req := testRequest(&genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh})
	req.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
		Mode:                 genai.FunctionCallingConfigModeAny,
		AllowedFunctionNames: []string{"save"},
	}}
	params, err := m.convertRequest(req)
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	if params.Thinking.OfAdaptive != nil || params.Thinking.OfEnabled != nil || params.OutputConfig.Effort != "" {
		t.Fatalf("forced tool request kept reasoning: thinking=%+v effort=%q", params.Thinking, params.OutputConfig.Effort)
	}
}

func TestVercelGateway_RequestAndResponseMetadata(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requestBody <- decoded
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"zai/glm-5.3-flash",
			"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":4,"output_tokens":2},
			"providerMetadata":{"gateway":{"cost":"0.0123","generationId":"gen_1","routing":{
				"originalModelId":"zai/glm-5.3-flash","resolvedProvider":"baseten","canonicalSlug":"zai/glm-5.3-flash",
				"modelAttemptCount":1,"totalProviderAttemptCount":2
			}}}
		}`)
	}))
	defer srv.Close()

	client := anthropic.NewClient(option.WithAPIKey("gateway-key"), option.WithBaseURL(srv.URL))
	llm, err := newTestModel(testConfig{
		Client:         client,
		CanonicalModel: "glm-5.3-flash",
		RequestModel:   "zai/glm-5.3-flash",
	},
		withReasoning(ReasoningConfig{DefaultLevel: genai.ThinkingLevelMedium}),
		withVercelGateway(adkmodels.VercelConfig{
			Only:              []string{"zai", "baseten"},
			Caching:           adkmodels.GatewayCachingAuto,
			ZeroDataRetention: true,
		}),
	)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}

	var response *model.LLMResponse
	for resp, err := range llm.GenerateContent(t.Context(), testRequest(nil), false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		response = resp
	}

	decoded := <-requestBody
	if decoded["model"] != "zai/glm-5.3-flash" {
		t.Fatalf("request model = %v", decoded["model"])
	}
	providerOptions := decoded["providerOptions"].(map[string]any)
	gateway := providerOptions["gateway"].(map[string]any)
	if gateway["zeroDataRetention"] != true {
		t.Fatalf("zeroDataRetention = %v, want true", gateway["zeroDataRetention"])
	}
	if gateway["caching"] != "auto" {
		t.Fatalf("caching = %v, want auto", gateway["caching"])
	}
	if _, ok := decoded["thinking"]; ok {
		t.Fatalf("Anthropic thinking = %v, want omitted", decoded["thinking"])
	}
	zai := providerOptions["zai"].(map[string]any)
	if got := zai["reasoningEffort"]; got != "high" {
		t.Fatalf("Z.AI reasoning effort = %v, want high", got)
	}

	metadata, ok := adkmodels.MetadataFromResponse(response)
	if !ok {
		t.Fatal("missing typed Vercel metadata")
	}
	if metadata.ResolvedProvider != "baseten" || metadata.CostUSD == nil || *metadata.CostUSD != 0.0123 {
		t.Fatalf("metadata = %+v", metadata)
	}
	responseMetadata, ok := adkmodels.MetadataFromResponse(response)
	if !ok || responseMetadata.ResponseID != "msg_1" {
		t.Fatalf("Anthropic response metadata = %+v, found=%t", responseMetadata, ok)
	}
}

func TestVercelGateway_ProviderNativeReasoningUsesRequestOverrides(t *testing.T) {
	requestBodies := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requestBodies <- decoded
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"zai/glm-5.3-flash",
			"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":4,"output_tokens":2}
		}`)
	}))
	defer srv.Close()

	client := anthropic.NewClient(option.WithAPIKey("gateway-key"), option.WithBaseURL(srv.URL))
	llm, err := newTestModel(testConfig{
		Client: client, CanonicalModel: "glm-5.3-flash", RequestModel: "zai/glm-5.3-flash",
	},
		withDefaultMaxTokens(64_000),
		withReasoning(ReasoningConfig{DefaultLevel: genai.ThinkingLevelHigh}),
		withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: true}),
	)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}

	requests := []*model.LLMRequest{
		testRequest(&genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}),
		testRequest(nil),
	}
	requests[0].Config.MaxOutputTokens = 4096
	requests[1].Config.MaxOutputTokens = 8192
	for _, request := range requests {
		for _, err := range llm.GenerateContent(t.Context(), request, false) {
			if err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}
		}
	}

	assertZAIEffort := func(wantEffort string, wantMaxTokens float64) {
		t.Helper()
		decoded := <-requestBodies
		if got := decoded["max_tokens"]; got != wantMaxTokens {
			t.Fatalf("max_tokens = %v, want %.0f", got, wantMaxTokens)
		}
		if _, ok := decoded["thinking"]; ok {
			t.Fatalf("Anthropic thinking = %v, want omitted", decoded["thinking"])
		}
		if outputConfig, ok := decoded["output_config"].(map[string]any); ok {
			if _, hasEffort := outputConfig["effort"]; hasEffort {
				t.Fatalf("Anthropic output_config.effort = %v, want omitted", outputConfig["effort"])
			}
		}
		providerOptions := decoded["providerOptions"].(map[string]any)
		zai := providerOptions["zai"].(map[string]any)
		if got := zai["reasoningEffort"]; got != wantEffort {
			t.Fatalf("Z.AI reasoningEffort = %v, want %q", got, wantEffort)
		}
	}

	assertZAIEffort("low", 4096)
	assertZAIEffort("max", 8192)
}

func TestVercelGateway_StreamingUsesProviderNativeRequestOptions(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requestBody <- decoded
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, successSSE)
	}))
	defer srv.Close()

	client := anthropic.NewClient(option.WithAPIKey("gateway-key"), option.WithBaseURL(srv.URL))
	llm, err := newTestModel(testConfig{
		Client: client, CanonicalModel: "glm-5.3-flash", RequestModel: "zai/glm-5.3-flash",
	},
		withDefaultMaxTokens(64_000),
		withReasoning(ReasoningConfig{DefaultLevel: genai.ThinkingLevelHigh}),
		withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: true}),
	)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}

	request := testRequest(&genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow, IncludeThoughts: true})
	request.Config.MaxOutputTokens = 4096
	for _, err := range llm.GenerateContent(t.Context(), request, true) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
	}

	decoded := <-requestBody
	if got := decoded["max_tokens"]; got != float64(4096) {
		t.Fatalf("max_tokens = %v, want 4096", got)
	}
	if got := decoded["stream"]; got != true {
		t.Fatalf("stream = %v, want true", got)
	}
	if _, ok := decoded["thinking"]; ok {
		t.Fatalf("Anthropic thinking = %v, want omitted", decoded["thinking"])
	}
	providerOptions := decoded["providerOptions"].(map[string]any)
	gateway := providerOptions["gateway"].(map[string]any)
	if got := gateway["zeroDataRetention"]; got != true {
		t.Fatalf("zeroDataRetention = %v, want true", got)
	}
	zai := providerOptions["zai"].(map[string]any)
	if got := zai["reasoningEffort"]; got != "low" {
		t.Fatalf("Z.AI reasoningEffort = %v, want low", got)
	}
}

func TestVercelGateway_RetentionAllowedIsExplicit(t *testing.T) {
	m := mustTestModel(t, "claude-test", withVercelGateway(adkmodels.VercelConfig{ZeroDataRetention: false}))
	options, err := gateway.Options(*m.vercel, m.family, "", false, adkmodels.OpenAIReasoningConfig{})
	if err != nil {
		t.Fatalf("WireProviderOptions() error = %v", err)
	}
	gateway := options["gateway"]
	if gateway["zeroDataRetention"] != false {
		t.Fatalf("zeroDataRetention = %v, want false", gateway["zeroDataRetention"])
	}
}

func TestPromptCachingModes(t *testing.T) {
	breakpoint := &CacheBreakpoint{}
	_, err := NewModel(Config{Client: testClient(), Model: adkmodels.ModelConfig{CanonicalModel: "claude-test", PromptCaching: adkmodels.PromptCachingConfig{Anthropic: adkmodels.AnthropicPromptCachingConfig{Tools: breakpoint}}}})
	if err == nil || !strings.Contains(err.Error(), "breakpoints require manual mode") {
		t.Fatalf("invalid cache config: %v", err)
	}

	m := mustTestModel(t, "claude-test", withPromptCaching(PromptCachingConfig{
		Mode: PromptCacheManual,
		Auto: breakpoint,
	}))
	params, err := m.convertRequest(testRequest(nil))
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	if params.CacheControl.Type == "" {
		t.Fatal("manual prompt cache breakpoint is missing")
	}
}

func TestConvertRequest_SetsStructuredOutput(t *testing.T) {
	m := mustTestModel(t, "claude-test")
	req := testRequest(nil)
	req.Config.ResponseSchema = &genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{"answer": {Type: genai.TypeString}},
		Required:   []string{"answer"},
	}
	params, err := m.convertRequest(req)
	if err != nil {
		t.Fatalf("convertRequest() error = %v", err)
	}
	if params.OutputConfig.Format.Schema == nil {
		t.Fatal("structured output schema is missing")
	}
}

func TestFilterThoughtParts_PreservesProviderState(t *testing.T) {
	parts := []*genai.Part{
		{Text: "unsigned", Thought: true},
		{Text: "signed", Thought: true, ThoughtSignature: []byte("sig")},
		{Text: "redacted", Thought: true, PartMetadata: map[string]any{"provider": "state"}},
		{Text: "answer"},
	}
	got := filterThoughtParts(parts)
	if len(got) != 3 || got[0].Text != "signed" || got[1].Text != "redacted" || got[2].Text != "answer" {
		t.Fatalf("filterThoughtParts() = %+v", got)
	}
}

func TestFilterThoughtParts_PreservesThoughtOnlyTurn(t *testing.T) {
	got := filterThoughtParts([]*genai.Part{{Text: "hidden", Thought: true}})
	if len(got) != 1 || !got[0].Thought || got[0].Text != "" {
		t.Fatalf("filterThoughtParts() = %+v", got)
	}
}

func testClient(opts ...option.RequestOption) anthropic.Client {
	return anthropic.NewClient(append([]option.RequestOption{option.WithAPIKey("test-key")}, opts...)...)
}

func mustTestModel(t *testing.T, canonical anthropic.Model, options ...testOption) *anthropicModel {
	t.Helper()
	llm, err := newTestModel(testConfig{Client: testClient(), CanonicalModel: canonical}, options...)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}
	return llm.(*anthropicModel)
}

func testRequest(thinking *genai.ThinkingConfig) *model.LLMRequest {
	return &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("Hello", "user")},
		Config:   &genai.GenerateContentConfig{ThinkingConfig: thinking},
	}
}
