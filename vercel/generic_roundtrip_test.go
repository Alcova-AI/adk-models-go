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
)

func TestGenericGatewayToolRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("ai-language-model-id") != "xiaomi/mimo-v2.6-pro" || req.Header.Get("ai-language-model-streaming") != fmt.Sprint(stream) {
					t.Fatal("incorrect routing headers")
				}
				if calls == 2 {
					var body struct {
						Prompt []struct {
							Role    string
							Content []map[string]any
						}
					}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if len(body.Prompt) != 3 || len(body.Prompt[1].Content) != 2 {
						t.Fatalf("unexpected prompt: %+v", body.Prompt)
					}
					thought := body.Prompt[1].Content[0]
					call := body.Prompt[1].Content[1]
					result := body.Prompt[2].Content[0]
					if thought["type"] != "reasoning" || thought["text"] != "retrieve the code" || call["toolCallId"] != "call_1" || result["toolCallId"] != "call_1" || result["type"] != "tool-result" {
						t.Fatalf("history changed: %+v", body.Prompt)
					}
				}
				if !stream {
					if calls == 1 {
						return jsonResponse(req, `{"content":[{"type":"reasoning","text":"retrieve the code"},{"type":"tool-call","toolCallId":"call_1","toolName":"lookup","input":"{}"}],"finishReason":{"unified":"tool-calls","raw":"tool_calls"}}`), nil
					}
					return jsonResponse(req, `{"content":[{"type":"text","text":"MIMO-437"}],"finishReason":{"unified":"stop","raw":"stop"}}`), nil
				}
				events := []string{
					`data: {"type":"text-start","id":"text_1"}`,
					`data: {"type":"text-delta","id":"text_1","delta":"MIMO-437"}`,
					`data: {"type":"text-end","id":"text_1"}`,
					`data: {"type":"finish","finishReason":{"unified":"stop","raw":"stop"}}`, "",
				}
				if calls == 1 {
					events = []string{
						`data: {"type":"reasoning-start","id":"reason_1"}`,
						`data: {"type":"reasoning-delta","id":"reason_1","delta":"retrieve the code"}`,
						`data: {"type":"reasoning-end","id":"reason_1"}`,
						`data: {"type":"tool-call","toolCallId":"call_1","toolName":"lookup","input":"{}"}`,
						`data: {"type":"finish","finishReason":{"unified":"tool-calls","raw":"tool_calls"}}`, "",
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.Join(events, "\n\n"))), Request: req}, nil
			})
			llm, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{CanonicalModel: "mimo-v2.6-pro", RequestModel: "xiaomi/mimo-v2.6-pro"}})
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Find the code", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup"}}}}}}
			var response *model.LLMResponse
			for r, err := range llm.GenerateContent(t.Context(), req, stream) {
				if err != nil {
					t.Fatal(err)
				}
				if r != nil && (!stream || r.TurnComplete) {
					response = r
				}
			}
			if response == nil || response.Content == nil || len(response.Content.Parts) != 2 {
				t.Fatalf("missing tool response: %+v", response)
			}
			if p := response.Content.Parts[0]; p.Text != "" || len(p.ThoughtSignature) == 0 {
				t.Fatal("hidden reasoning state lost")
			}
			if call := response.Content.Parts[1].FunctionCall; call == nil || call.ID != "call_1" {
				t.Fatal("tool call lost")
			}
			req.Contents = append(req.Contents, response.Content, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "call_1", Name: "lookup", Response: map[string]any{"code": "MIMO-437"}}}}})
			for r, err := range llm.GenerateContent(t.Context(), req, stream) {
				if err != nil {
					t.Fatal(err)
				}
				if r != nil && (!stream || r.TurnComplete) {
					response = r
				}
			}
			if calls != 2 || response.Content.Parts[0].Text != "MIMO-437" {
				t.Fatalf("missing final answer: %+v", response)
			}
		})
	}
}
