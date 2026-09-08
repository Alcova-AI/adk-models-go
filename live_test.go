// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Run explicitly with AI_GATEWAY_API_KEY and ADK_MODELS_LIVE=1. These synthetic
// requests verify remote acceptance of the Luna workaround replacement.
func TestLunaMinimalLive(t *testing.T) {
	key := os.Getenv("AI_GATEWAY_API_KEY")
	if key == "" || os.Getenv("ADK_MODELS_LIVE") != "1" {
		t.Skip("explicit live integration settings are not set")
	}
	cfg := adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{}}
	sdk := openai.NewClient(option.WithAPIKey(key), option.WithBaseURL("https://ai-gateway.vercel.sh/v1"))
	responses, err := adkopenai.NewModel(adkopenai.Config{Client: sdk, Model: cfg})
	if err != nil {
		t.Fatal(err)
	}
	native, err := adkvercel.NewModel(adkvercel.Config{APIKey: key, Model: cfg})
	if err != nil {
		t.Fatal(err)
	}
	for name, llm := range map[string]model.LLM{"responses": responses, "native": native} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", name, stream), func(t *testing.T) {
				request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Reply with exactly OK.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 64, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}}}
				complete := false
				for response, err := range llm.GenerateContent(t.Context(), request, stream) {
					if err != nil {
						t.Fatal(err)
					}
					if response != nil && !response.Partial && response.Content != nil && len(response.Content.Parts) > 0 {
						complete = true
					}
				}
				if !complete {
					t.Fatal("no completed response")
				}
				if request.Config.ThinkingConfig.ThinkingLevel != genai.ThinkingLevelMinimal {
					t.Fatal("caller level changed")
				}
			})
		}
	}
}

// TestGeminiVertexGatewayLive verifies the Anthropic-compatible gateway route,
// including the lowercase thinking-level encoding required by Vertex options.
func TestGeminiVertexGatewayLive(t *testing.T) {
	key := os.Getenv("AI_GATEWAY_API_KEY")
	if key == "" || os.Getenv("ADK_MODELS_LIVE") != "1" {
		t.Skip("explicit live integration settings are not set")
	}
	sdk := anthropic.NewClient(anthropicoption.WithAPIKey(key), anthropicoption.WithBaseURL("https://ai-gateway.vercel.sh"))
	llm, err := adkanthropic.NewModel(adkanthropic.Config{Client: sdk, Model: adkmodels.ModelConfig{CanonicalModel: "gemini-3.7-flash", RequestModel: "google/gemini-3.7-flash", DefaultMaxOutputTokens: 64000, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelHigh}, Vercel: &adkmodels.VercelConfig{Only: []string{"vertex"}, ZeroDataRetention: true}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("structured/stream=%t", stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Return a JSON object with status equal to OK.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}, ResponseSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"status": {Type: genai.TypeString}}, Required: []string{"status"}}}}
			// The SDK requires streaming at the large default output limit.
			if !stream {
				req.Config.MaxOutputTokens = 4096
			}
			text := ""
			for resp, err := range llm.GenerateContent(ctx, req, stream) {
				if err != nil {
					t.Fatal(err)
				}
				if resp != nil && !resp.Partial && resp.Content != nil {
					for _, part := range resp.Content.Parts {
						if !part.Thought {
							text += part.Text
						}
					}
				}
			}
			var result map[string]string
			if err := json.Unmarshal([]byte(text), &result); err != nil {
				t.Fatal(err)
			}
			if result["status"] != "OK" {
				t.Fatalf("unexpected status %q", result["status"])
			}
		})
	}
}
