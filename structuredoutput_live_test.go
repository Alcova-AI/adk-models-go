// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkmodels_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	"github.com/Alcova-AI/adk-models-go/structuredoutput"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// TestStructuredOutputLiveMatrix sends at most 16 synthetic requests, with no SDK
// retries and a per-request timeout. It tests real provider output through the
// public extractor, not whether a provider happens to emit commentary on demand.
// Run explicitly with ADK_STRUCTURED_LIVE=1 and all three provider credentials.
func TestStructuredOutputLiveMatrix(t *testing.T) {
	if os.Getenv("ADK_STRUCTURED_LIVE") != "1" {
		t.Skip("set ADK_STRUCTURED_LIVE=1 for synthetic provider requests")
	}
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AI_GATEWAY_API_KEY"} {
		if os.Getenv(key) == "" {
			t.Fatalf("required credential %s is missing", key)
		}
	}
	routes := []matrixRoute{
		{"openai-direct-responses", "openai", "direct", "gpt-5.6-luna", "gpt-5.6-luna"},
		{"openai-direct-chat", "openai", "direct-chat", "gpt-5.6-luna", "gpt-5.6-luna"},
		{"openai-vercel-responses", "openai", "vercel-openai", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"openai-vercel-chat", "openai", "vercel-chat", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"openai-vercel-native", "openai", "vercel-native", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"anthropic-direct-messages", "anthropic", "direct", "claude-haiku-5-5", "claude-haiku-5-5"},
		{"anthropic-vercel-messages", "anthropic", "vercel-anthropic", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"},
		{"anthropic-vercel-native", "anthropic", "vercel-native", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"},
	}
	for _, route := range routes {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", route.Name, stream), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
				defer cancel()
				tr := &matrixTransport{base: http.DefaultTransport}
				mc := adkmodels.ModelConfig{CanonicalModel: route.Model, RequestModel: route.Request, DefaultMaxOutputTokens: 1024}
				if route.Provider == "openai" {
					mc.Reasoning.DefaultLevel = genai.ThinkingLevelMinimal
				}
				if route.Path == "vercel-native" || route.Path == "vercel-openai" || route.Path == "vercel-chat" || route.Path == "vercel-anthropic" {
					mc.Vercel = &adkmodels.VercelConfig{Only: []string{route.Provider}, ZeroDataRetention: true}
				}
				var llm model.LLM
				var err error
				if route.Path == "direct-chat" || route.Path == "vercel-chat" {
					key, base := os.Getenv("OPENAI_API_KEY"), "https://api.openai.com/v1"
					if route.Path == "vercel-chat" {
						key, base = os.Getenv("AI_GATEWAY_API_KEY"), "https://ai-gateway.vercel.sh/v1"
					}
					client := openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(base), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: tr, Timeout: 60 * time.Second}))
					llm, err = adkopenai.NewModel(adkopenai.Config{Client: client, API: adkopenai.APIChatCompletions, Model: mc})
				} else {
					llm, err = matrixConfiguredModel(ctx, route, toolschema.Config{}, tr, mc, 60*time.Second)
				}
				if err != nil {
					t.Fatal(err)
				}
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Synthetic meeting: Jordan will send the review letter by 30 October 2026. The agreed decision is Cobalt Mango. Return only JSON with decision equal to Cobalt Mango and owner equal to Jordan.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 1024, ResponseSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"decision": {Type: genai.TypeString}, "owner": {Type: genai.TypeString}}, Required: []string{"decision", "owner"}}}}
				var final *model.LLMResponse
				partials := 0
				for resp, err := range llm.GenerateContent(ctx, req, stream) {
					if err != nil {
						t.Fatal(err)
					}
					if resp != nil {
						if resp.Partial {
							partials++
						} else {
							final = resp
						}
					}
				}
				if final == nil || final.Content == nil || final.FinishReason != genai.FinishReasonStop {
					t.Fatalf("missing successful final: %+v", final)
				}
				before, err := json.Marshal(final.Content)
				if err != nil {
					t.Fatal(err)
				}
				got, err := structuredoutput.ExtractJSON[struct {
					Decision string `json:"decision"`
					Owner    string `json:"owner"`
				}](final.Content)
				if err != nil {
					t.Fatal(err)
				}
				if got.Decision != "Cobalt Mango" || got.Owner != "Jordan" {
					t.Fatalf("wrong extracted values: %+v", got)
				}
				after, err := json.Marshal(final.Content)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("extraction changed response")
				}
				if stream && partials == 0 {
					t.Fatal("stream yielded no partial responses")
				}
				md, _ := adkmodels.MetadataFromResponse(final)
				t.Logf("route=%s model=%s stream=%t partials=%d statuses=%v response_id=%s resolved_provider=%s extracted=%+v", route.Name, route.Request, stream, partials, tr.statuses, md.ResponseID, md.ResolvedProvider, got)
				if mc.Vercel != nil && md.ResolvedProvider != "" && md.ResolvedProvider != route.Provider {
					t.Fatalf("wrong gateway provider %s", md.ResolvedProvider)
				}
			})
		}
	}
}
