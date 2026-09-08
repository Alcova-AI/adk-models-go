// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

func TestGenerateUsesNativeV4Contract(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got, want := request.URL.String(), "https://gateway.invalid/v4/ai/language-model"; got != want {
			t.Fatalf("URL = %q, want %q", got, want)
		}
		wantHeaders := map[string]string{
			"Authorization": "Bearer gateway-key", "ai-gateway-protocol-version": "0.0.1",
			"ai-gateway-auth-method": "api-key", "ai-language-model-specification-version": "4",
			"ai-language-model-id": "openai/gpt-5.6-luna", "ai-language-model-streaming": "false",
		}
		for key, want := range wantHeaders {
			if got := request.Header.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, exists := body["model"]; exists {
			t.Errorf("model must be in the header, body = %#v", body)
		}
		prompt := body["prompt"].([]any)
		if len(prompt) != 3 {
			t.Fatalf("prompt length = %d, want 3", len(prompt))
		}
		if body["reasoning"] != nil {
			t.Errorf("reasoning = %v", body["reasoning"])
		}
		providerOptions := body["providerOptions"].(map[string]any)
		gatewayOptions := providerOptions["gateway"].(map[string]any)
		if gatewayOptions["zeroDataRetention"] != true || gatewayOptions["order"].([]any)[0] != "openai" {
			t.Errorf("gateway provider options = %#v", gatewayOptions)
		}
		openAIOptions := providerOptions["openai"].(map[string]any)
		if openAIOptions["reasoningEffort"] != "low" || openAIOptions["store"] != false || openAIOptions["customOption"] != "value" {
			t.Errorf("OpenAI provider options = %#v", openAIOptions)
		}
		return jsonResponse(request, `{
			"content":[
				{"type":"reasoning","text":"brief thought","providerMetadata":{"openai":{"itemId":"rs_1","reasoningEncryptedContent":"opaque"}}},
				{"type":"text","text":"done"},
				{"type":"tool-call","toolCallId":"call_1","toolName":"lookup","input":"{\"id\":7}"}
			],
			"finishReason":{"unified":"tool-calls","raw":"tool_calls"},
			"usage":{"inputTokens":{"total":100,"noCache":10,"cacheRead":80,"cacheWrite":10},"outputTokens":{"total":10,"text":5,"reasoning":5}},
			"providerMetadata":{"gateway":{"generationId":"gen_1","cost":"0.0123","routing":{"resolvedProvider":"openai","originalModelId":"openai/gpt-5.6-luna","canonicalSlug":"openai/gpt-5.6-luna","modelAttemptCount":1,"totalProviderAttemptCount":2}}},
			"response":{"id":"resp_1","modelId":"openai/gpt-5.6-luna"}
		}`), nil
	})
	llm, err := NewModel(Config{APIKey: "gateway-key", BaseURL: "https://gateway.invalid/v4/ai/", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{ZeroDataRetention: true, Order: []string{"openai"}, ProviderOptions: map[string]map[string]any{"openai": {"customOption": "value"}}}, PromptCaching: adkmodels.PromptCachingConfig{OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheExplicit, Key: "session-agent"}}}})

	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "read"}, {InlineData: &genai.Blob{Data: []byte("pdf"), MIMEType: "application/pdf", DisplayName: "brief.pdf"}}}},
			{Role: string(genai.RoleModel), Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "lookup", ID: "old_call", Args: map[string]any{"id": 1}}}}},
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "lookup", ID: "old_call", Response: map[string]any{"ok": true}}}}},
		},
		Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}},
	}
	var response *model.LLMResponse
	for value, err := range llm.GenerateContent(t.Context(), request, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		response = value
	}
	if response == nil || response.Content.Parts[1].Text != "done" {
		t.Fatalf("response = %#v", response)
	}
	if got := response.UsageMetadata.CachedContentTokenCount; got != 80 {
		t.Errorf("cached tokens = %d, want 80", got)
	}
	if got := response.Content.Parts[2].FunctionCall.Args["id"]; got != float64(7) {
		t.Errorf("tool id = %#v", got)
	}
	if len(response.Content.Parts[0].ThoughtSignature) == 0 || response.Content.Parts[0].Text != "" {
		t.Errorf("reasoning state = %#v", response.Content.Parts[0])
	}
	metadata, ok := adkmodels.MetadataFromResponse(response)
	if !ok || metadata.ProviderMetadata["gateway"] == nil {
		t.Fatalf("metadata = %#v, found = %t", metadata, ok)
	}
	if metadata.GenerationID != "gen_1" || metadata.ResolvedProvider != "openai" || metadata.OriginalModelID != "openai/gpt-5.6-luna" || metadata.CanonicalModel != "openai/gpt-5.6-luna" || metadata.ModelAttemptCount != 1 || metadata.ProviderAttemptCount != 2 {
		t.Errorf("normalized metadata = %#v", metadata)
	}
	if metadata.CostUSD == nil || *metadata.CostUSD != 0.0123 || metadata.CacheWriteInputTokens != 10 {
		t.Errorf("cost/cache metadata = %#v", metadata)
	}
}

func TestStreamYieldsTextToolAndFinish(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("ai-language-model-streaming") != "true" {
			t.Errorf("streaming header missing")
		}
		stream := strings.Join([]string{
			`data: {"type":"response-metadata","id":"resp_1","modelId":"openai/gpt-5.6-luna"}`,
			`data: {"type":"text-start","id":"text_1"}`,
			`data: {"type":"text-delta","id":"text_1","delta":"hello"}`,
			`data: {"type":"text-end","id":"text_1"}`,
			`data: {"type":"tool-call","toolCallId":"call_1","toolName":"lookup","input":"{\"id\":1}"}`,
			`data: {"type":"finish","finishReason":{"unified":"tool-calls","raw":"tool_calls"},"usage":{"inputTokens":{"total":9,"cacheRead":4,"cacheWrite":2},"outputTokens":{"total":3,"reasoning":1}},"providerMetadata":{"gateway":{"generationId":"gen_stream"}}}`,
			"",
		}, "\n\n")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream)), Request: request}, nil
	})
	llm, err := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna"}, APIKey: "key", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("ping", genai.RoleUser)}}
	var responses []*model.LLMResponse
	for response, err := range llm.GenerateContent(t.Context(), request, true) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 3 {
		t.Fatalf("response count = %d, want 3", len(responses))
	}
	if responses[0].Content.Parts[0].Text != "hello" || !responses[0].Partial {
		t.Errorf("text response = %#v", responses[0])
	}
	if responses[1].Content.Parts[0].FunctionCall.Name != "lookup" {
		t.Errorf("tool response = %#v", responses[1])
	}
	if !responses[2].TurnComplete || responses[2].UsageMetadata.CachedContentTokenCount != 4 {
		t.Errorf("finish response = %#v", responses[2])
	}
	if got := responses[2].Content.Parts; len(got) != 2 || got[0].Text != "hello" || got[1].FunctionCall == nil || got[1].FunctionCall.Name != "lookup" {
		t.Errorf("finish content = %#v, want accumulated text and tool call", got)
	}
	metadata, ok := adkmodels.MetadataFromResponse(responses[2])
	if !ok || metadata.ResponseID != "resp_1" || metadata.GenerationID != "gen_stream" || metadata.CacheWriteInputTokens != 2 {
		t.Errorf("finish metadata = %#v, found = %t", metadata, ok)
	}
	if got := responses[2].ModelVersion; got != "openai/gpt-5.6-luna" {
		t.Errorf("model version = %q", got)
	}
}

func TestStreamReasoningSignatureUsesEndMetadata(t *testing.T) {
	state := streamState{reasoning: map[string]*strings.Builder{}, reasoningMetadata: map[string]map[string]any{}}
	_, _, err := state.convert(protocol.StreamPart{
		Type: "reasoning-start", ID: "reasoning_1",
		ProviderMetadata: map[string]any{"openai": map[string]any{"itemId": "rs_1", "reasoningEncryptedContent": nil}},
	}, false)
	if err != nil {
		t.Fatalf("reasoning start: %v", err)
	}
	_, _, err = state.convert(protocol.StreamPart{Type: "reasoning-delta", ID: "reasoning_1", Delta: "summary"}, false)
	if err != nil {
		t.Fatalf("reasoning delta: %v", err)
	}
	response, _, err := state.convert(protocol.StreamPart{
		Type: "reasoning-end", ID: "reasoning_1",
		ProviderMetadata: map[string]any{"openai": map[string]any{"itemId": "rs_1", "reasoningEncryptedContent": "opaque"}},
	}, false)
	if err != nil {
		t.Fatalf("reasoning end: %v", err)
	}
	if response == nil || len(response.Content.Parts) != 1 {
		t.Fatalf("response = %#v", response)
	}
	replayed, err := decodeReasoningPart(response.Content.Parts[0].ThoughtSignature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	openAI := replayed.ProviderOptions["openai"]
	if got := openAI["reasoningEncryptedContent"]; got != "opaque" {
		t.Fatalf("encrypted content = %#v", got)
	}
}

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestGatewayError(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`)), Request: request}, nil
	})
	llm, _ := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-test"}, APIKey: "key", HTTPClient: &http.Client{Transport: transport}})
	request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("ping", genai.RoleUser)}}
	for _, err := range llm.GenerateContent(t.Context(), request, false) {
		if got := fmt.Sprint(err); got != "vercel gateway returned 429: slow down" {
			t.Fatalf("error = %q", got)
		}
	}
}

func TestGenerationConfigForwardsSeed(t *testing.T) {
	for _, test := range []struct {
		name string
		seed *int32
	}{{"unset", nil}, {"zero", new(int32(0))}, {"nonzero", new(int32(42))}} {
		t.Run(test.name, func(t *testing.T) {
			seed := test.seed
			var sent map[string]any
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(request.Body).Decode(&sent); err != nil {
					t.Fatal(err)
				}
				return jsonResponse(request, `{"content":[{"type":"text","text":"ok"}],"finishReason":{"unified":"stop"}}`), nil
			})
			llm, err := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-test"}, APIKey: "test", HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Seed: seed}}
			for _, err := range llm.GenerateContent(t.Context(), req, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
			got, exists := sent["seed"]
			if seed == nil {
				if exists {
					t.Errorf("unset seed sent as %v", got)
				}
				return
			}
			if !exists || got != float64(*seed) {
				t.Errorf("seed = %v, want %d", got, *seed)
			}
		})
	}
}

func TestGenerationConfigRejectsUnsupportedThinkingBeforeRequest(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  *genai.ThinkingConfig
	}{
		{"zero budget", &genai.ThinkingConfig{ThinkingBudget: new(int32(0))}},
		{"budget with level", &genai.ThinkingConfig{ThinkingBudget: new(int32(100)), ThinkingLevel: genai.ThinkingLevelHigh}},
		{"unknown level", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevel("unknown")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := test.cfg
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				t.Error("invalid reasoning configuration reached transport")
				return jsonResponse(request, `{"content":[{"type":"text","text":"ok"}],"finishReason":{"unified":"stop"}}`), nil
			})
			llm, err := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-test"}, APIKey: "test", HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: cfg}}
			var gotError bool
			for _, err := range llm.GenerateContent(t.Context(), req, false) {
				gotError = gotError || err != nil
			}
			if !gotError {
				t.Error("unsupported thinking configuration accepted")
			}
		})
	}
}

func TestGenerationConfigUsesThinkingLevelsWithoutBudget(t *testing.T) {
	for level, want := range map[genai.ThinkingLevel]string{
		"": "", genai.ThinkingLevelUnspecified: "",
		genai.ThinkingLevelMinimal: "minimal", genai.ThinkingLevelLow: "low",
		genai.ThinkingLevelMedium: "medium", genai.ThinkingLevelHigh: "high",
	} {
		t.Run(string(level), func(t *testing.T) {
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: level}}}
			got, err := buildCallOptions(req, 100)
			if err != nil {
				t.Fatal(err)
			}
			if got.Reasoning != want {
				t.Errorf("reasoning = %q, want %q", got.Reasoning, want)
			}
			if req.Config.ThinkingConfig.ThinkingBudget != nil {
				t.Error("budget was introduced")
			}
		})
	}
}
