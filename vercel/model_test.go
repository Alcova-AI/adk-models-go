// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/toolschema"

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

func TestOpenAIStructuredSchemaIsStrictWithoutMutatingCaller(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			nested := &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"status": {Type: genai.TypeString}}, Required: []string{"status"}}
			cfg := &genai.GenerateContentConfig{ResponseSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"result": nested}, Required: []string{"result"}}}
			if raw {
				cfg.ResponseJsonSchema = map[string]any{"type": "object", "properties": map[string]any{"result": map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}}, "required": []string{"status"}}}, "required": []string{"result"}}
				cfg.ResponseSchema = nil
			}
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				schema := body["responseFormat"].(map[string]any)["schema"].(map[string]any)
				child := schema["properties"].(map[string]any)["result"].(map[string]any)
				for _, obj := range []map[string]any{schema, child} {
					if obj["additionalProperties"] != false {
						t.Fatalf("schema is not strict: %#v", obj)
					}
					if len(obj["required"].([]any)) != 1 {
						t.Fatalf("missing required property: %#v", obj)
					}
				}
				return jsonResponse(request, `{"content":[{"type":"text","text":"done"}],"finishReason":{"unified":"stop","raw":"stop"}}`), nil
			})
			llm, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range llm.GenerateContent(t.Context(), &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("JSON", genai.RoleUser)}, Config: cfg}, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
			after, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("caller schema changed")
			}
		})
	}
}

func TestGenerateZeroArgumentToolSchema(t *testing.T) {
	for _, tt := range []struct {
		name        string
		declaration *genai.FunctionDeclaration
	}{
		{"typed", &genai.FunctionDeclaration{Name: "list_skills", Parameters: &genai.Schema{Type: genai.TypeObject}}},
		{"raw", &genai.FunctionDeclaration{Name: "list_skills", ParametersJsonSchema: json.RawMessage(`{"type":"object"}`)}},
		{"explicit", &genai.FunctionDeclaration{Name: "list_skills", ParametersJsonSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}},
		{"omitted", &genai.FunctionDeclaration{Name: "list_skills"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				var body protocol.CallOptions
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body.Tools) != 1 || body.Tools[0].Name != "list_skills" {
					t.Fatalf("tools = %#v", body.Tools)
				}
				properties, ok := body.Tools[0].InputSchema["properties"].(map[string]any)
				if !ok || len(properties) != 0 {
					t.Fatalf("zero-argument schema = %#v", body.Tools[0].InputSchema)
				}
				return jsonResponse(req, `{"content":[{"type":"text","text":"OK"}],"finishReason":{"unified":"stop","raw":"stop"}}`), nil
			})
			llm, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{
				CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna",
				ToolSchemas: toolschema.Config{AllowUnsupported: true, Warn: func(context.Context, toolschema.Warning) {}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("List skills", genai.RoleUser)}, Config: &genai.GenerateContentConfig{
				Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{tt.declaration}}},
			}}
			for _, err := range llm.GenerateContent(t.Context(), req, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("HTTP calls = %d", calls)
			}
		})
	}
}

func TestSelectedSystemCacheBoundaryUsesSuppliedNamespaces(t *testing.T) {
	for _, index := range []int{0, 1} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("question", genai.RoleUser)}, Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "shared"}, {Text: "dynamic"}}}}}
			calls := 0
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var body protocol.CallOptions
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				want := "shared"
				if index == 1 {
					want = "shareddynamic"
				}
				if body.Prompt[0].Content != want {
					t.Fatalf("prefix = %#v", body.Prompt[0])
				}
				for _, ns := range []string{"azure", "openai"} {
					if !reflect.DeepEqual(body.Prompt[0].ProviderOptions[ns]["promptCacheBreakpoint"], map[string]any{"mode": "explicit"}) {
						t.Fatalf("missing %s marker: %#v", ns, body.Prompt[0])
					}
					if body.ProviderOptions[ns]["promptCacheOptions"] == nil {
						t.Fatalf("missing %s request settings", ns)
					}
				}
				if len(body.Prompt[1].ProviderOptions) != 0 {
					t.Fatal("suffix was marked")
				}
				return jsonResponse(r, `{"content":[{"type":"text","text":"OK"}],"finishReason":{"unified":"stop"}}`), nil
			})
			m, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", PromptCaching: adkmodels.PromptCachingConfig{SystemInstructionPartIndex: &index}, Vercel: &adkmodels.VercelConfig{
				ProviderOptions:               map[string]map[string]any{"azure": {"promptCacheOptions": map[string]any{"mode": "explicit", "ttl": "30m"}}, "openai": {"promptCacheOptions": map[string]any{"mode": "explicit", "ttl": "30m"}}},
				SystemInstructionCacheOptions: map[string]map[string]any{"azure": {"promptCacheBreakpoint": map[string]any{"mode": "explicit"}}, "openai": {"promptCacheBreakpoint": map[string]any{"mode": "explicit"}}},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range m.GenerateContent(t.Context(), req, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 || len(req.Config.SystemInstruction.Parts) != 2 || req.Config.SystemInstruction.Parts[0].Text != "shared" {
				t.Fatal("request mutated or not sent")
			}
		})
	}
}

func TestNativeAnthropicSuppliedCacheMarkers(t *testing.T) {
	index := 0
	markers := map[string]map[string]any{"anthropic": {"cacheControl": map[string]any{"type": "ephemeral", "ttl": "1h"}}}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prompt := body["prompt"].([]any)
		if !reflect.DeepEqual(prompt[0].(map[string]any)["providerOptions"], map[string]any{"anthropic": map[string]any{"cacheControl": map[string]any{"type": "ephemeral", "ttl": "1h"}}}) {
			t.Fatalf("system marker = %#v", prompt[0])
		}
		if _, marked := prompt[1].(map[string]any)["providerOptions"]; marked {
			t.Fatal("dynamic suffix marked")
		}
		part := prompt[2].(map[string]any)["content"].([]any)[0].(map[string]any)
		if part["providerOptions"] == nil {
			t.Fatal("history marker dropped")
		}
		return jsonResponse(r, `{"content":[{"type":"text","text":"OK"}],"finishReason":{"unified":"stop"}}`), nil
	})
	m, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{CanonicalModel: "claude-sonnet-4-6", RequestModel: "anthropic/claude-sonnet-4-6", PromptCaching: adkmodels.PromptCachingConfig{SystemInstructionPartIndex: &index}, Vercel: &adkmodels.VercelConfig{SystemInstructionCacheOptions: markers, ConversationHistoryCacheOptions: markers}}})
	if err != nil {
		t.Fatal(err)
	}
	markers["anthropic"]["cacheControl"].(map[string]any)["ttl"] = "5m"
	index = 1
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("question", genai.RoleUser)}, Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "shared"}, {Text: "dynamic"}}}}}
	for _, err := range m.GenerateContent(t.Context(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTypedCacheModesPreserveNativeMarkerMaps(t *testing.T) {
	for _, mode := range []adkmodels.OpenAIPromptCacheMode{adkmodels.OpenAIPromptCacheImplicit, adkmodels.OpenAIPromptCacheExplicit} {
		t.Run(string(mode), func(t *testing.T) {
			index := 0
			marker := map[string]map[string]any{"azure": {"promptCacheBreakpoint": map[string]any{"mode": "explicit"}}}
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				prompt := body["prompt"].([]any)
				shared := prompt[0].(map[string]any)
				if shared["content"] != "shared" || !reflect.DeepEqual(shared["providerOptions"], map[string]any{"azure": map[string]any{"promptCacheBreakpoint": map[string]any{"mode": "explicit"}}}) {
					t.Fatalf("shared marker: %#v", shared)
				}
				if runtime := prompt[1].(map[string]any); runtime["providerOptions"] != nil {
					t.Fatalf("runtime suffix marked: %#v", runtime)
				}
				count := 1
				for _, message := range prompt[2:] {
					for _, part := range message.(map[string]any)["content"].([]any) {
						if options := part.(map[string]any)["providerOptions"]; options != nil {
							if !reflect.DeepEqual(options, shared["providerOptions"]) {
								t.Fatalf("history marker: %#v", options)
							}
							count++
						}
					}
				}
				want := 4
				if mode == adkmodels.OpenAIPromptCacheImplicit {
					want = 3
				}
				if count != want {
					t.Fatalf("markers = %d, want %d", count, want)
				}
				return jsonResponse(request, `{"content":[{"type":"text","text":"OK"}],"finishReason":{"unified":"stop","raw":"stop"}}`), nil
			})
			llm, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{
				CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna",
				Vercel:        &adkmodels.VercelConfig{SystemInstructionCacheOptions: marker, ConversationHistoryCacheOptions: marker},
				PromptCaching: adkmodels.PromptCachingConfig{SystemInstructionPartIndex: &index, OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: mode}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "shared"}, {Text: "runtime"}}}}}
			for range 8 {
				req.Contents = append(req.Contents, genai.NewContentFromText("history", genai.RoleUser))
			}
			for _, err := range llm.GenerateContent(t.Context(), req, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
