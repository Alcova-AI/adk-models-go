// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestCompatibleChatRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, vercel := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/vercel=%t", stream, vercel), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Content-Type", "application/json")
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if r.URL.Path != "/chat/completions" || body["model"] != "gemma-4-31B-it" || body["max_completion_tokens"] != float64(8192) {
						t.Errorf("bad route or token limit: %v", body)
					}
					for _, key := range []string{"store", "max_tokens", "reasoning_effort"} {
						if _, ok := body[key]; ok {
							t.Errorf("unexpected %s", key)
						}
					}
					tool := body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
					if _, ok := tool["strict"]; ok {
						t.Error("strict decoding enabled")
					}
					schema := tool["parameters"].(map[string]any)
					prop := schema["properties"].(map[string]any)["id"].(map[string]any)
					if prop["pattern"] != "^[0-9]+$" {
						t.Error("schema constraint lost")
					}
					if calls == 2 {
						messages := body["messages"].([]any)
						last := messages[len(messages)-1].(map[string]any)
						if last["tool_call_id"] != "call_1" {
							t.Error("tool ID lost")
						}
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						if calls == 1 {
							fmt.Fprint(w, "data: {\"id\":\"c\",\"model\":\"gemma-4-31B-it\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"id\\\":\\\"42\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
						} else {
							fmt.Fprint(w, "data: {\"id\":\"c\",\"model\":\"gemma-4-31B-it\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Found 42\"},\"finish_reason\":\"stop\"}]}\n\n")
						}
						fmt.Fprint(w, "data: {\"id\":\"c\",\"model\":\"gemma-4-31B-it\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n\n")
					} else if calls == 1 {
						fmt.Fprint(w, `{"model":"gemma-4-31B-it","choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":\"42\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":15}}`)
					} else {
						fmt.Fprint(w, `{"model":"gemma-4-31B-it","choices":[{"message":{"content":"Found 42"},"finish_reason":"stop"}],"usage":{"total_tokens":15}}`)
					}
				}))
				defer server.Close()
				cfg := Config{API: APIChatCompletions, Client: sdk.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL), option.WithMaxRetries(0)), Model: adkmodels.ModelConfig{CanonicalModel: "gemma-4-31B-it", DefaultMaxOutputTokens: 8192}}
				if vercel {
					cfg.Model.Vercel = &adkmodels.VercelConfig{}
				}
				llm, err := NewModel(cfg)
				if err != nil {
					t.Fatal(err)
				}
				req := chatTestRequest()
				req.Config.MaxOutputTokens = 0
				req.Config.Tools = []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "pattern": "^[0-9]+$"}}}}}}}
				first := chatCollect(t, llm, req, stream)
				if first.ModelVersion != "gemma-4-31B-it" || first.UsageMetadata.TotalTokenCount != 15 {
					t.Fatal("metadata lost")
				}
				call := first.Content.Parts[0].FunctionCall
				if call == nil || call.Args["id"] != "42" {
					t.Fatal("tool arguments lost")
				}
				req.Contents = append(req.Contents, first.Content, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"value": "42"}}}}})
				final := chatCollect(t, llm, req, stream)
				if final.Content.Parts[0].Text != "Found 42" || calls != 2 {
					t.Fatal("round trip failed")
				}
			})
		}
	}
}

func TestCompatibleChatValidation(t *testing.T) {
	client := sdk.NewClient(option.WithAPIKey("test"), option.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid request reached transport")
		return nil, fmt.Errorf("unexpected request")
	})}))
	for _, name := range []string{"", "vendor/gemma", "two names"} {
		if _, err := NewModel(Config{API: APIChatCompletions, Client: client, Model: adkmodels.ModelConfig{CanonicalModel: name}}); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	base := adkmodels.ModelConfig{CanonicalModel: "gemma-4-31B-it"}
	for _, mutate := range []func(*adkmodels.ModelConfig){func(c *adkmodels.ModelConfig) { c.RequestModel = "gpt-6-luna" }, func(c *adkmodels.ModelConfig) { c.DefaultMaxOutputTokens = -1 }, func(c *adkmodels.ModelConfig) { c.Reasoning.DefaultLevel = genai.ThinkingLevelHigh }, func(c *adkmodels.ModelConfig) { c.PromptCaching.OpenAI.Key = "key" }} {
		c := base
		mutate(&c)
		if _, err := NewModel(Config{API: APIChatCompletions, Client: client, Model: c}); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if _, err := NewModel(Config{Client: client, Model: base}); err != nil {
		t.Fatalf("Responses rejected unmapped model: %v", err)
	}
	llm, err := NewModel(Config{API: APIChatCompletions, Client: client, Model: base})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []*model.LLMRequest{
		{Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}}},
		{Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true}}},
		{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup", ParametersJsonSchema: map[string]any{"type": "object", "unknownKeyword": true}}}}}}},
	} {
		req.Contents = chatTestRequest().Contents
		failed := false
		for _, err := range llm.GenerateContent(t.Context(), req, false) {
			failed = err != nil
		}
		if !failed {
			t.Fatal("invalid request accepted")
		}
	}
}
