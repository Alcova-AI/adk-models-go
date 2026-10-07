// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// TestChatLiveMatrix is deliberately opt-in even when credentials exist. It
// sends synthetic inputs only, disables retries and bounds request count/output.
// ADK_CHAT_LIVE_ROUTE can select direct, vercel-openai, vercel-azure or vercel-google.
func TestChatLiveMatrix(t *testing.T) {
	if os.Getenv("ADK_CHAT_LIVE") != "1" {
		t.Skip("set ADK_CHAT_LIVE=1 to spend up to USD 0.10 on synthetic live checks")
	}
	var calls int
	var reserved, estimated float64
	for _, route := range []string{"direct", "vercel-openai", "vercel-azure", "vercel-google"} {
		if selected := os.Getenv("ADK_CHAT_LIVE_ROUTE"); selected != "" && selected != route {
			continue
		}
		if !t.Run(route, func(t *testing.T) {
			name, wire, key, base := "gpt-6-luna", "gpt-6-luna", os.Getenv("OPENAI_API_KEY"), "https://api.openai.com/v1"
			if requested := os.Getenv("ADK_CHAT_LIVE_MODEL"); requested != "" {
				name = requested
				wire = name
			}
			inputRate, outputRate := 0.000000125, 0.0000005 // conservative cache-write input rate
			var gateway *adkmodels.VercelConfig
			level := genai.ThinkingLevelMinimal
			if route != "direct" {
				key = os.Getenv("AI_GATEWAY_API_KEY")
				base = "https://ai-gateway.vercel.sh/v1"
				wire = "openai/" + name
				gateway = &adkmodels.VercelConfig{Only: []string{"openai"}, ZeroDataRetention: true}
			}
			if route == "vercel-azure" {
				gateway.Only = []string{"azure"}
			}
			if route == "vercel-google" {
				name = "gemini-3.1-flash-lite"
				wire = "google/" + name
				gateway.Only = []string{"vertex"}
				level = genai.ThinkingLevelLow
				inputRate = 0.000000275
				outputRate = 0.00000165
			}
			if key == "" {
				t.Fatal("required route credential is missing")
			}
			client := sdk.NewClient(option.WithAPIKey(key), option.WithBaseURL(base), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}))
			makeModel := func(api API) model.LLM {
				llm, err := NewModel(Config{API: api, Client: client, Model: adkmodels.ModelConfig{CanonicalModel: name, RequestModel: wire, DefaultMaxOutputTokens: 256, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: level}, Vercel: gateway}})
				if err != nil {
					t.Fatal(err)
				}
				return llm
			}
			llm := makeModel(APIChatCompletions)
			invoke := func(t *testing.T, llm model.LLM, r *model.LLMRequest, stream bool) *model.LLMResponse {
				t.Helper()
				if route == "vercel-google" {
					r.Config.MaxOutputTokens = 1024
				}
				limit := r.Config.MaxOutputTokens
				if limit == 0 {
					limit = 256
				}
				// Fixtures contain under 5,000 input tokens including the tiny image.
				bound := 5000*inputRate + float64(limit)*outputRate
				if calls >= 64 || reserved+bound > 0.10 {
					t.Fatal("live test budget exhausted")
				}
				reserved += bound
				calls++
				var final *model.LLMResponse
				partials := 0
				var partialText strings.Builder
				for response, err := range llm.GenerateContent(t.Context(), r, stream) {
					if err != nil {
						t.Fatal(err)
					}
					if response.Partial {
						partials++
						if response.Content != nil {
							for _, part := range response.Content.Parts {
								partialText.WriteString(part.Text)
							}
						}
					} else {
						final = response
					}
				}
				if final == nil || final.Content == nil || final.FinishReason != genai.FinishReasonStop {
					t.Fatalf("incomplete response: %+v", final)
				}
				if stream {
					var finalText strings.Builder
					for _, part := range final.Content.Parts {
						if !part.Thought {
							finalText.WriteString(part.Text)
						}
					}
					if finalText.Len() > 0 && (partials == 0 || finalText.String() != partialText.String()) {
						t.Fatal("streamed text missing or differs from final")
					}
				}
				usage := final.UsageMetadata
				if usage == nil || usage.TotalTokenCount <= 0 {
					t.Fatal("missing token usage")
				}
				cost := float64(usage.PromptTokenCount)*inputRate + float64(usage.CandidatesTokenCount)*outputRate
				estimated += cost
				md, _ := adkmodels.MetadataFromResponse(final)
				t.Logf("call=%d stream=%t partials=%d input=%d output=%d reasoning=%d estimated_usd=%.7f provider=%s response_id=%s", calls, stream, partials, usage.PromptTokenCount, usage.CandidatesTokenCount, usage.ThoughtsTokenCount, cost, md.ResolvedProvider, md.ResponseID)
				if md.ResolvedProvider != "" && gateway != nil && md.ResolvedProvider != gateway.Only[0] {
					t.Fatalf("unexpected provider %q", md.ResolvedProvider)
				}
				return final
			}
			text := func(r *model.LLMResponse) string {
				var b strings.Builder
				for _, p := range r.Content.Parts {
					b.WriteString(p.Text)
				}
				return b.String()
			}
			for _, stream := range []bool{false, true} {
				if !t.Run(fmt.Sprintf("text/stream=%t", stream), func(t *testing.T) {
					r := chatTestRequest()
					r.Contents[0] = genai.NewContentFromText("Reply with exactly OK", genai.RoleUser)
					if !strings.Contains(text(invoke(t, llm, r, stream)), "OK") {
						t.Fatal("wrong text")
					}
				}) {
					t.FailNow()
				}
				if !t.Run(fmt.Sprintf("tools/stream=%t", stream), func(t *testing.T) {
					r := chatTestRequest()
					r.Config.Tools = chatTestTools()
					if route == "vercel-google" {
						// Gemini does not support the closed-object tool constraint.
						delete(r.Config.Tools[0].FunctionDeclarations[0].ParametersJsonSchema.(map[string]any), "additionalProperties")
					}
					r.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"lookup"}}}
					first := invoke(t, llm, r, stream)
					var call *genai.FunctionCall
					for _, p := range first.Content.Parts {
						if p.FunctionCall != nil {
							if call != nil {
								t.Fatal("expected one tool call")
							}
							call = p.FunctionCall
						}
					}
					if call == nil || call.Name != "lookup" || call.ID == "" || call.Args["id"] != "42" {
						t.Fatalf("wrong tool call: %+v", call)
					}
					r.Contents = append(r.Contents, first.Content, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"value": "synthetic-result-42"}}}}})
					r.Config.ToolConfig.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeNone
					r.Config.ToolConfig.FunctionCallingConfig.AllowedFunctionNames = nil
					final := invoke(t, llm, r, stream)
					for _, p := range final.Content.Parts {
						if p.FunctionCall != nil {
							t.Fatal("tool called while disabled")
						}
					}
					if !strings.Contains(text(final), "42") {
						t.Fatal("tool result not used")
					}
				}) {
					t.FailNow()
				}
				if !t.Run(fmt.Sprintf("schema/stream=%t", stream), func(t *testing.T) {
					r := chatTestRequest()
					r.Contents[0] = genai.NewContentFromText("Return JSON with ok set to true.", genai.RoleUser)
					r.Config.ResponseJsonSchema = map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}
					var out map[string]any
					if err := json.Unmarshal([]byte(text(invoke(t, llm, r, stream))), &out); err != nil || len(out) != 1 || out["ok"] != true {
						t.Fatalf("schema output %v: %v", out, err)
					}
				}) {
					t.FailNow()
				}
			}
			if !t.Run("image", func(t *testing.T) {
				img := image.NewRGBA(image.Rect(0, 0, 32, 32))
				for y := 0; y < 32; y++ {
					for x := 0; x < 32; x++ {
						img.Set(x, y, color.RGBA{R: 255, A: 255})
					}
				}
				var encoded bytes.Buffer
				if err := png.Encode(&encoded, img); err != nil {
					t.Fatal(err)
				}
				r := chatTestRequest()
				r.Contents = []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "What colour is this image? Answer one word."}, {InlineData: &genai.Blob{MIMEType: "image/png", Data: encoded.Bytes()}}}}}
				if !strings.Contains(strings.ToLower(text(invoke(t, llm, r, false))), "red") {
					t.Fatal("image colour incorrect")
				}
			}) {
				t.FailNow()
			}
			if !t.Run("reasoning", func(t *testing.T) {
				r := chatTestRequest()
				r.Config.MaxOutputTokens = 1024
				r.Config.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}
				r.Contents[0] = genai.NewContentFromText("What is 17 multiplied by 19? Reply with the number only.", genai.RoleUser)
				if !strings.Contains(text(invoke(t, llm, r, true)), "323") {
					t.Fatal("wrong arithmetic answer")
				}
			}) {
				t.FailNow()
			}
			if route == "direct" || (route != "vercel-google" && os.Getenv("ADK_CHAT_LIVE_CACHE_PROBE") == "1") {
				if !t.Run("cache-boundary", func(t *testing.T) {
					index := 0
					makeCached := func(mode adkmodels.OpenAIPromptCacheMode) model.LLM {
						m, err := NewModel(Config{API: APIChatCompletions, Client: client, Model: adkmodels.ModelConfig{CanonicalModel: name, RequestModel: wire, DefaultMaxOutputTokens: 64, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: level}, Vercel: gateway, PromptCaching: adkmodels.PromptCachingConfig{SystemInstructionPartIndex: &index, OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: mode, SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}}}}})
						if err != nil {
							t.Fatal(err)
						}
						return m
					}
					shared := fmt.Sprintf("Synthetic cache run %d. ", time.Now().UnixNano()) + strings.Repeat("Shared fictional instruction context. ", 400)
					r := &model.LLMRequest{Config: &genai.GenerateContentConfig{MaxOutputTokens: 64, SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: shared}, {Text: "Synthetic user context A."}}}}, Contents: []*genai.Content{genai.NewContentFromText("Reply only OK.", genai.RoleUser)}}
					explicit := makeCached(adkmodels.OpenAIPromptCacheExplicit)
					first := invoke(t, explicit, r, false)
					r.Config.SystemInstruction.Parts[1].Text = "Different synthetic user context B."
					r.Contents[0] = genai.NewContentFromText("A different question. Reply only OK.", genai.RoleUser)
					second := invoke(t, explicit, r, true)
					if second.UsageMetadata.CachedContentTokenCount < first.UsageMetadata.PromptTokenCount-256 {
						t.Fatalf("explicit shared prefix not reused: input=%d cached=%d", first.UsageMetadata.PromptTokenCount, second.UsageMetadata.CachedContentTokenCount)
					}
					implicit := makeCached(adkmodels.OpenAIPromptCacheImplicit)
					r.Contents = []*genai.Content{genai.NewContentFromText(strings.Repeat("Fictional discussion history entry. ", 250)+"Reply only OK.", genai.RoleUser)}
					history := invoke(t, implicit, r, false)
					r.Contents = append(r.Contents, history.Content, genai.NewContentFromText("Continue. Reply only OK.", genai.RoleUser))
					appended := invoke(t, implicit, r, true)
					if appended.UsageMetadata.CachedContentTokenCount < history.UsageMetadata.PromptTokenCount-128 {
						t.Fatalf("appended history not reused: input=%d cached=%d", history.UsageMetadata.PromptTokenCount, appended.UsageMetadata.CachedContentTokenCount)
					}
					r.Contents = []*genai.Content{genai.NewContentFromText("Unrelated new conversation. Reply only OK.", genai.RoleUser)}
					fresh := invoke(t, implicit, r, false)
					if appended.UsageMetadata.CachedContentTokenCount < fresh.UsageMetadata.CachedContentTokenCount+500 {
						t.Fatal("cache hits did not include conversation history")
					}
					t.Logf("shared_cached=%d history_cached=%d fresh_cached=%d", second.UsageMetadata.CachedContentTokenCount, appended.UsageMetadata.CachedContentTokenCount, fresh.UsageMetadata.CachedContentTokenCount)
				}) {
					t.FailNow()
				}
			}
			if route != "vercel-google" {
				if !t.Run("responses-regression", func(t *testing.T) {
					m := makeModel(APIResponses)
					r := chatTestRequest()
					r.Config.Tools = chatTestTools()
					if route == "vercel-google" {
						// Gemini does not support the closed-object tool constraint.
						delete(r.Config.Tools[0].FunctionDeclarations[0].ParametersJsonSchema.(map[string]any), "additionalProperties")
					}
					r.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}}
					first := invoke(t, m, r, true)
					var call *genai.FunctionCall
					for _, p := range first.Content.Parts {
						if p.FunctionCall != nil {
							call = p.FunctionCall
						}
					}
					if call == nil {
						t.Fatal("missing Responses tool call")
					}
					r.Contents = append(r.Contents, first.Content, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"value": "42"}}}}})
					r.Config.ToolConfig.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeNone
					invoke(t, m, r, true)
				}) {
					t.FailNow()
				}
			}
		}) {
			t.FailNow()
		}
	}
	t.Logf("TOTAL calls=%d conservative_reserved_usd=%.6f usage_estimate_usd=%.6f", calls, reserved, estimated)
}
