// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func chatTestRequest() *model.LLMRequest {
	return &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Look up 42", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 128}}
}
func chatTestTools() []*genai.Tool {
	return []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup", Description: "Look up an item", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []string{"id"}, "additionalProperties": false}}}}}
}
func chatTestModel(t *testing.T, handler http.HandlerFunc, v *adkmodels.VercelConfig) model.LLM {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	llm, err := NewModel(Config{API: APIChatCompletions, Client: sdk.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL), option.WithMaxRetries(0)), Model: adkmodels.ModelConfig{CanonicalModel: "gpt-6-luna", Vercel: v, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelMinimal}}})
	if err != nil {
		t.Fatal(err)
	}
	return llm
}
func chatCollect(t *testing.T, llm model.LLM, req *model.LLMRequest, stream bool) *model.LLMResponse {
	t.Helper()
	var final *model.LLMResponse
	var partialText strings.Builder
	for r, err := range llm.GenerateContent(t.Context(), req, stream) {
		if err != nil {
			t.Fatal(err)
		}
		if !r.Partial {
			final = r
		} else if r.Content != nil {
			for _, p := range r.Content.Parts {
				partialText.WriteString(p.Text)
			}
		}
	}
	if final == nil {
		t.Fatal("missing final response")
	}
	if stream && len(final.Content.Parts) > 0 && final.Content.Parts[0].Text != "" {
		if partialText.String() != final.Content.Parts[0].Text {
			t.Fatalf("partial text %q does not match final", partialText.String())
		}
	}
	return final
}
func TestChatWireAndToolRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, vercel := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/vercel=%t", stream, vercel), func(t *testing.T) {
				var count int
				var v *adkmodels.VercelConfig
				if vercel {
					v = &adkmodels.VercelConfig{Only: []string{"openai"}, ZeroDataRetention: true}
				}
				llm := chatTestModel(t, func(w http.ResponseWriter, r *http.Request) {
					count++
					if r.URL.Path != "/chat/completions" {
						t.Errorf("path %s", r.URL.Path)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["max_completion_tokens"] != float64(128) || body["store"] != false {
						t.Errorf("missing request limits: %v", body)
					}
					if vercel {
						opts := body["providerOptions"].(map[string]any)
						if opts["gateway"].(map[string]any)["zeroDataRetention"] != true {
							t.Error("missing retention")
						}
					} else if body["reasoning_effort"] != "none" {
						t.Error("missing reasoning")
					}
					if count == 2 {
						messages := body["messages"].([]any)
						if messages[len(messages)-1].(map[string]any)["tool_call_id"] != "call_1" {
							t.Error("lost call id")
						}
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						if count == 1 {
							fmt.Fprint(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"id\\\":\"}}]}}]}\n\n")
							fmt.Fprint(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"42\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
						} else {
							fmt.Fprint(w, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Found 42\"},\"finish_reason\":\"stop\"}]}\n\n")
						}
						fmt.Fprint(w, "data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n\n")
					} else if count == 1 {
						fmt.Fprint(w, `{"id":"c1","choices":[{"index":0,"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":\"42\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
					} else {
						fmt.Fprint(w, `{"id":"c1","choices":[{"index":0,"message":{"content":"Found 42"},"finish_reason":"stop"}]}`)
					}
				}, v)
				req := chatTestRequest()
				req.Config.Tools = chatTestTools()
				first := chatCollect(t, llm, req, stream)
				call := first.Content.Parts[0].FunctionCall
				if call == nil || call.ID != "call_1" || call.Args["id"] != "42" {
					t.Fatalf("bad tool call: %+v", first)
				}
				if first.UsageMetadata.TotalTokenCount != 15 {
					t.Fatal("lost stream usage")
				}
				req.Contents = append(req.Contents, first.Content, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"value": "42"}}}}})
				second := chatCollect(t, llm, req, stream)
				if second.Content.Parts[0].Text != "Found 42" {
					t.Fatal("bad final text")
				}
			})
		}
	}
}
func TestChatInvalidRequestNeverSends(t *testing.T) {
	cases := map[string]func(*model.LLMRequest){
		"reasoning tools": func(r *model.LLMRequest) {
			r.Config.Tools = chatTestTools()
			r.Config.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}
		},
		"thought output": func(r *model.LLMRequest) { r.Config.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: true} },
		"unknown allowed tool": func(r *model.LLMRequest) {
			r.Config.Tools = chatTestTools()
			r.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"missing"}}}
		},
		"orphan result": func(r *model.LLMRequest) {
			r.Contents = []*genai.Content{{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "missing", Name: "lookup"}}}}}
		},
		"reasoning history": func(r *model.LLMRequest) { r.Contents[0].Parts[0].ThoughtSignature = []byte("state") },
		"assistant before tool result": func(r *model.LLMRequest) {
			r.Contents = []*genai.Content{
				{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "a", Name: "lookup"}}}},
				{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "b", Name: "lookup"}}}},
				{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "a", Name: "lookup"}}, {FunctionResponse: &genai.FunctionResponse{ID: "b", Name: "lookup"}}}},
			}
		},
		"unsupported topK": func(r *model.LLMRequest) { r.Config.TopK = new(float32(1)) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			llm := chatTestModel(t, func(w http.ResponseWriter, r *http.Request) { calls++ }, nil)
			req := chatTestRequest()
			change(req)
			var got error
			for _, err := range llm.GenerateContent(t.Context(), req, false) {
				got = err
			}
			if got == nil || calls != 0 {
				t.Fatalf("error=%v HTTP calls=%d", got, calls)
			}
		})
	}
}
func TestChatModelRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name, effort     string
		tools, wantError bool
	}{{"gpt-6-luna", "", true, true}, {"gpt-6-sol-2026-09-22", "low", true, true}, {"gpt-6-luna", "none", true, false}, {"gpt-6-astra", "none", true, true}, {"gpt-6-astra", "high", false, false}, {"gpt-6-luna-future", "high", true, false}} {
		if got := validateChatToolReasoning(tc.name, tc.effort, tc.tools); (got != nil) != tc.wantError {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
func TestChatStreamFailure(t *testing.T) {
	for _, body := range []string{"data: {broken}\n\n", "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"} {
		llm := chatTestModel(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, body)
		}, nil)
		var got error
		for _, err := range llm.GenerateContent(t.Context(), chatTestRequest(), true) {
			if err != nil {
				got = err
			}
		}
		if got == nil {
			t.Fatal("accepted broken stream")
		}
	}
}
func TestChatTimeout(t *testing.T) {
	client := sdk.NewClient(option.WithAPIKey("test"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}))
	llm, err := NewModel(Config{API: APIChatCompletions, Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-6-luna"}})
	if err != nil {
		t.Fatal(err)
	}
	req := chatTestRequest()
	req.Config.HTTPOptions = &genai.HTTPOptions{Timeout: new(20 * time.Millisecond)}
	var got error
	for _, err := range llm.GenerateContent(t.Context(), req, false) {
		got = err
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("timeout error %v", got)
	}
}
func TestChatAPISelection(t *testing.T) {
	cfg := Config{Client: sdk.NewClient(option.WithAPIKey("test")), Model: adkmodels.ModelConfig{CanonicalModel: "gpt-6-luna"}}
	for _, api := range []API{"", APIResponses, APIChatCompletions, "bad"} {
		cfg.API = api
		m, err := NewModel(cfg)
		if api == "bad" {
			if err == nil {
				t.Fatal("accepted unknown API")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		_, chat := m.(*chatModel)
		if chat != (api == APIChatCompletions) {
			t.Fatal("wrong API")
		}
	}
}
func TestChatToolChoiceAndSchema(t *testing.T) {
	llm := chatTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		tools := body["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["function"].(map[string]any)["name"] != "lookup" {
			t.Error("allowed tool filtering failed")
		}
		if body["tool_choice"] != "required" {
			t.Error("tool choice missing")
		}
		format := body["response_format"].(map[string]any)
		if format["json_schema"].(map[string]any)["strict"] != true {
			t.Error("strict schema missing")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`)
	}, nil)
	req := chatTestRequest()
	req.Config.Tools = chatTestTools()
	req.Config.Tools[0].FunctionDeclarations = append(req.Config.Tools[0].FunctionDeclarations, &genai.FunctionDeclaration{Name: "excluded", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}})
	req.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"lookup"}}}
	req.Config.ResponseJsonSchema = map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}
	if !strings.Contains(chatCollect(t, llm, req, false).Content.Parts[0].Text, "true") {
		t.Fatal("missing text")
	}
}
