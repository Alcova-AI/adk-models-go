// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkvercel

import (
	"encoding/json"
	"net/http"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestNativeGatewayModelNames(t *testing.T) {
	for _, tt := range []struct {
		canonical, request string
		bad                bool
	}{
		{"mimo-v2.6-pro", "xiaomi/mimo-v2.6-pro", false},
		{"gemma-4-31B-it", "google/gemma-4-31B-it", false},
		{"deepseek-v3.2", "deepseek/deepseek-v3.2", false},
		{"future-model", "future/model", false},
		{"gemini-3.7-flash", "google/gemini-3.7-flash", false},
		{"", "xiaomi/mimo-v2.6-pro", true},
		{"xiaomi/mimo-v2.6-pro", "", true},
		{"mimo v2.6", "xiaomi/mimo-v2.6-pro", true},
		{"mimo-v2.6-pro", "openai/gpt-5.6-luna", true},
		{"gemini-3.7-flash", "openai/gpt-5.6-luna", true},
	} {
		t.Run(tt.canonical+"/"+tt.request, func(t *testing.T) {
			m, err := NewModel(Config{APIKey: "test", Model: adkmodels.ModelConfig{CanonicalModel: tt.canonical, RequestModel: tt.request}})
			if (err != nil) != tt.bad {
				t.Fatalf("NewModel error = %v, want error %t", err, tt.bad)
			}
			if err == nil && m.Name() != tt.canonical {
				t.Fatalf("model name = %q", m.Name())
			}
		})
	}
}

func TestGenericGatewayPreservesSchemaAndOptions(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"code":{"type":"string","pattern":"^[A-Z]+$","minLength":2},"count":{"type":"integer","minimum":1}},"required":["code"],"additionalProperties":false}`)
	calls := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("ai-language-model-id") != "xiaomi/mimo-v2.6-pro" {
			t.Errorf("wrong request model")
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		tool := body["tools"].([]any)[0].(map[string]any)
		got, _ := json.Marshal(tool["inputSchema"])
		var want map[string]any
		_ = json.Unmarshal(schema, &want)
		encoded, _ := json.Marshal(want)
		if string(got) != string(encoded) {
			t.Errorf("schema changed: %s", got)
		}
		if _, exists := tool["strict"]; exists {
			t.Errorf("generic route must not promise strict decoding: %v", tool)
		}
		opts := body["providerOptions"].(map[string]any)
		if len(opts) != 2 || opts["xiaomi"].(map[string]any)["customOption"] != "value" {
			t.Errorf("unexpected provider mappings: %v", opts)
		}
		if opts["gateway"].(map[string]any)["zeroDataRetention"] != false {
			t.Errorf("unexpected data policy: %v", opts)
		}
		return jsonResponse(req, `{"content":[{"type":"text","text":"OK"}],"finishReason":{"unified":"stop","raw":"stop"}}`), nil
	})
	m, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{
		CanonicalModel: "mimo-v2.6-pro", RequestModel: "xiaomi/mimo-v2.6-pro",
		Vercel: &adkmodels.VercelConfig{ProviderOptions: map[string]map[string]any{"xiaomi": {"customOption": "value"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{
		Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup", ParametersJsonSchema: schema}}}},
	}}
	for _, err := range m.GenerateContent(t.Context(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("HTTP calls = %d", calls)
	}
	// A model without a mapping must reject requested thinking levels before HTTP.
	req.Config.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}
	failed := false
	for _, err := range m.GenerateContent(t.Context(), req, false) {
		failed = err != nil
	}
	if !failed || calls != 1 {
		t.Fatalf("explicit thinking accepted: error=%t calls=%d", failed, calls)
	}
}
