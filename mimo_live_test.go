// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestMiMoGatewayLive(t *testing.T) {
	if os.Getenv("ADK_MIMO_LIVE") != "1" {
		t.Skip("set ADK_MIMO_LIVE=1")
	}
	llm, err := adkvercel.NewModel(adkvercel.Config{APIKey: os.Getenv("AI_GATEWAY_API_KEY"), Model: adkmodels.ModelConfig{CanonicalModel: "mimo-v2.6-pro", RequestModel: "xiaomi/mimo-v2.6-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Call lookup_test_code to retrieve the test code, then reply with that code. Do not invent it.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 2048, Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup_test_code", Description: "Returns the test code.", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}}}}}}}}
			var call *genai.FunctionCall
			for resp, err := range llm.GenerateContent(ctx, req, stream) {
				if err != nil {
					t.Fatal(err)
				}
				if resp != nil && (!stream || resp.TurnComplete) && resp.Content != nil {
					req.Contents = append(req.Contents, resp.Content)
					for _, p := range resp.Content.Parts {
						if p.FunctionCall != nil {
							call = p.FunctionCall
						}
					}
				}
			}
			if call == nil || call.Name != "lookup_test_code" {
				t.Fatal("missing expected tool call")
			}
			req.Contents = append(req.Contents, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"code": "MIMO-437"}}}}})
			var answer string
			for resp, err := range llm.GenerateContent(ctx, req, stream) {
				if err != nil {
					t.Fatal(err)
				}
				if resp != nil && (!stream || resp.TurnComplete) && resp.Content != nil {
					for _, p := range resp.Content.Parts {
						if !p.Thought {
							answer += p.Text
						}
					}
				}
			}
			if !strings.Contains(answer, "MIMO-437") {
				t.Fatalf("expected code in answer, got %q", answer)
			}
			t.Log("MiMo tool call and follow-up response verified")
		})
	}
}
