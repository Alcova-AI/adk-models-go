// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestUnmappedModelsAcrossProtocols(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		for _, gateway := range []bool{false, true} {
			if adapter == "vercel" && !gateway {
				continue
			}
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/gateway=%t/stream=%t", adapter, gateway, stream), func(t *testing.T) {
					calls := 0
					schema := map[string]any{"type": "object", "properties": map[string]any{"code": map[string]any{"type": "string", "pattern": "^[A-Z]+$", "minLength": float64(2)}}, "required": []any{"code"}, "additionalProperties": false}
					client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							return nil, err
						}
						if adapter == "vercel" {
							if r.Header.Get("ai-language-model-id") != "vendor/custom-model" {
								t.Error("wrong route")
							}
						} else if body["model"] != "vendor/custom-model" {
							t.Error("model rewritten")
						}
						for _, key := range []string{"thinking", "reasoning", "include", "store"} {
							if _, ok := body[key]; ok {
								t.Errorf("unexpected inferred control %s", key)
							}
						}
						if gateway {
							providers := body["providerOptions"].(map[string]any)
							if len(providers) != 2 || providers["gateway"].(map[string]any)["zeroDataRetention"] != true || providers["vendor"].(map[string]any)["custom"] != "value" {
								t.Errorf("provider policy changed: %v", providers)
							}
						}
						tool := body["tools"].([]any)[0].(map[string]any)
						if _, ok := tool["strict"]; ok {
							t.Error("strict decoding enabled")
						}
						key := map[string]string{"anthropic": "input_schema", "openai": "parameters", "vercel": "inputSchema"}[adapter]
						if !reflect.DeepEqual(tool[key], schema) {
							t.Errorf("schema changed: %v", tool[key])
						}
						return wireResponse(r, adapter, stream), nil
					})}
					cfg := adkmodels.ModelConfig{CanonicalModel: "custom-model", RequestModel: "vendor/custom-model"}
					if gateway {
						cfg.Vercel = &adkmodels.VercelConfig{ZeroDataRetention: true, ProviderOptions: map[string]map[string]any{"vendor": {"custom": "value"}}}
					}
					llm, err := wireModel(adapter, client, cfg)
					if err != nil {
						t.Fatal(err)
					}
					request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup", ParametersJsonSchema: schema}}}}}}
					terminal := false
					for resp, err := range llm.GenerateContent(t.Context(), request, stream) {
						if err != nil {
							t.Fatal(err)
						}
						if resp != nil && (!stream || resp.TurnComplete) {
							terminal = true
						}
					}
					if !terminal || calls != 1 {
						t.Fatal("missing completed response")
					}
					for _, level := range []genai.ThinkingLevel{genai.ThinkingLevelMinimal, genai.ThinkingLevelHigh} {
						request.Config.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: level}
						failed := false
						for _, err := range llm.GenerateContent(t.Context(), request, stream) {
							failed = err != nil
						}
						if !failed || calls != 1 {
							t.Fatal("explicit reasoning reached transport")
						}
					}
					request.Config.ThinkingConfig = nil
					request.Config.Tools[0].FunctionDeclarations[0].ParametersJsonSchema = map[string]any{"type": "object", "unknownKeyword": true}
					failed := false
					for _, err := range llm.GenerateContent(t.Context(), request, stream) {
						failed = err != nil
					}
					if !failed || calls != 1 {
						t.Fatal("malformed schema reached transport")
					}
				})
			}
		}
	}
}

func TestUnmappedConfigurationValidation(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		for _, change := range []func(*adkmodels.ModelConfig){
			func(c *adkmodels.ModelConfig) { c.CanonicalModel = "" },
			func(c *adkmodels.ModelConfig) { c.CanonicalModel = "vendor/custom-model" },
			func(c *adkmodels.ModelConfig) { c.RequestModel = "openai/gpt-test" },
			func(c *adkmodels.ModelConfig) { c.Reasoning.DefaultLevel = genai.ThinkingLevelHigh },
			func(c *adkmodels.ModelConfig) { c.PromptCaching.OpenAI.Key = "key" },
			func(c *adkmodels.ModelConfig) { c.PromptCaching.Anthropic.Mode = adkmodels.AnthropicPromptCacheManual },
			func(c *adkmodels.ModelConfig) { c.Reasoning.OpenAI.Summary = "auto" },
		} {
			cfg := adkmodels.ModelConfig{CanonicalModel: "custom-model"}
			change(&cfg)
			if _, err := wireModel(adapter, &http.Client{}, cfg); err == nil {
				t.Fatalf("%s accepted invalid config %+v", adapter, cfg)
			}
		}
	}
}

func TestUnmappedToolRoundTrips(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai"} {
		for _, gateway := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/gateway=%t/stream=%t", adapter, gateway, stream), func(t *testing.T) {
					calls := 0
					client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						if calls == 2 {
							raw, err := io.ReadAll(r.Body)
							if err != nil {
								return nil, err
							}
							if !strings.Contains(string(raw), "call_1") || !strings.Contains(string(raw), "FOUND-42") {
								t.Errorf("tool result lost: %s", raw)
							}
							return wireResponse(r, adapter, stream), nil
						}
						body := `{"id":"resp_1","model":"custom-model","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"id\":\"42\"}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
						if adapter == "anthropic" {
							body = `{"id":"msg_1","type":"message","role":"assistant","model":"custom-model","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":{"id":"42"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`
						}
						contentType := "application/json"
						if stream {
							contentType = "text/event-stream"
							if adapter == "anthropic" {
								body = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + body + "}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
							} else {
								body = "data: {\"type\":\"response.completed\",\"response\":" + body + "}\n\ndata: [DONE]\n\n"
							}
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})}
					cfg := adkmodels.ModelConfig{CanonicalModel: "custom-model"}
					if gateway {
						cfg.Vercel = &adkmodels.VercelConfig{}
					}
					llm, err := wireModel(adapter, client, cfg)
					if err != nil {
						t.Fatal(err)
					}
					req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Lookup 42", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup"}}}}}}
					var first *model.LLMResponse
					for resp, err := range llm.GenerateContent(t.Context(), req, stream) {
						if err != nil {
							t.Fatal(err)
						}
						if resp != nil && (!stream || resp.TurnComplete) {
							first = resp
						}
					}
					if first == nil || first.Content == nil || len(first.Content.Parts) == 0 {
						t.Fatal("missing tool response")
					}
					call := first.Content.Parts[0].FunctionCall
					if call == nil || call.ID != "call_1" || call.Args["id"] != "42" {
						t.Fatalf("wrong tool call: %+v", call)
					}
					req.Contents = append(req.Contents, first.Content, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"code": "FOUND-42"}}}}})
					var final *model.LLMResponse
					for resp, err := range llm.GenerateContent(t.Context(), req, stream) {
						if err != nil {
							t.Fatal(err)
						}
						if resp != nil && (!stream || resp.TurnComplete) {
							final = resp
						}
					}
					if calls != 2 || final == nil || final.Content.Parts[0].Text != "ok" {
						t.Fatal("missing final answer")
					}
				})
			}
		}
	}
}
