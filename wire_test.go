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
	adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type wireTransport func(*http.Request) (*http.Response, error)

func (f wireTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// This matrix checks actual HTTP bodies rather than only the shared mapping.
// In particular, Luna MINIMAL must be none at both compatibility layers.
func TestGatewayReasoningWireMatrix(t *testing.T) {
	levels := []genai.ThinkingLevel{"", genai.ThinkingLevelMinimal, genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh, adkmodels.ThinkingLevelXHigh, adkmodels.ThinkingLevelMax}
	families := []struct {
		model, namespace string
		efforts          []string
	}{
		{"gpt-5.6-luna", "openai", []string{"", "none", "low", "medium", "high", "xhigh", "xhigh"}},
		{"claude-test", "anthropic", []string{"", "low", "low", "medium", "high", "xhigh", "max"}},
		{"gemini-test", "google", []string{"", "minimal", "low", "medium", "high", "high", "high"}},
		{"glm-test", "zai", []string{"", "low", "low", "high", "max", "max", "max"}},
	}
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		for _, family := range families {
			for i, level := range levels {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/stream=%t", adapter, family.model, level, stream), func(t *testing.T) {
						var body map[string]any
						client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								return nil, err
							}
							return wireResponse(r, adapter, stream), nil
						})}
						cfg := adkmodels.ModelConfig{CanonicalModel: family.model, RequestModel: family.namespace + "/" + family.model, Vercel: &adkmodels.VercelConfig{}}
						llm, err := wireModel(adapter, client, cfg)
						if err != nil {
							t.Fatal(err)
						}
						request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: level}}}
						before, _ := json.Marshal(request)
						count := 0
						for _, err := range llm.GenerateContent(t.Context(), request, stream) {
							if err != nil {
								t.Fatal(err)
							}
							count++
						}
						if count == 0 || body == nil {
							t.Fatal("no request or response")
						}
						after, _ := json.Marshal(request)
						if string(before) != string(after) {
							t.Fatal("caller request changed")
						}
						providers := body["providerOptions"].(map[string]any)
						if providers["gateway"].(map[string]any)["zeroDataRetention"] != false {
							t.Fatal("retention was not allowed by default")
						}
						values, _ := providers[family.namespace].(map[string]any)
						want := family.efforts[i]
						switch family.namespace {
						case "openai":
							assertWireValue(t, values, "reasoningEffort", want)
							if values["store"] != false {
								t.Fatal("OpenAI response storage enabled")
							}
							if adapter == "openai" {
								reasoning, _ := body["reasoning"].(map[string]any)
								assertWireValue(t, reasoning, "effort", want)
							}
						case "anthropic":
							assertWireValue(t, values, "effort", want)
							thinking, _ := values["thinking"].(map[string]any)
							mode := ""
							if i == 1 {
								mode = "disabled"
							} else if i > 1 {
								mode = "adaptive"
							}
							assertWireValue(t, thinking, "type", mode)
							if adapter == "anthropic" {
								native, _ := body["thinking"].(map[string]any)
								assertWireValue(t, native, "type", mode)
								output, _ := body["output_config"].(map[string]any)
								assertWireValue(t, output, "effort", want)
							}
						case "google":
							thinking, _ := values["thinkingConfig"].(map[string]any)
							assertWireValue(t, thinking, "thinkingLevel", want)
						case "zai":
							assertWireValue(t, values, "reasoningEffort", want)
						}
						if adapter == "openai" && body["store"] != false {
							t.Fatal("Responses storage enabled")
						}
						if adapter == "vercel" && body["reasoning"] != nil {
							t.Fatal("unmapped generic reasoning reached native gateway")
						}
					})
				}
			}
		}
	}
}
func assertWireValue(t *testing.T, values map[string]any, key, want string) {
	t.Helper()
	got := values[key]
	if want == "" {
		if got != nil {
			t.Errorf("%s=%v, want omitted", key, got)
		}
		return
	}
	if got != want {
		t.Errorf("%s=%v, want %q", key, got, want)
	}
}
func wireModel(adapter string, client *http.Client, cfg adkmodels.ModelConfig) (model.LLM, error) {
	switch adapter {
	case "anthropic":
		return adkanthropic.NewModel(adkanthropic.Config{Client: anthropic.NewClient(anthropicoption.WithAPIKey("test"), anthropicoption.WithBaseURL("https://gateway.invalid"), anthropicoption.WithHTTPClient(client)), Model: cfg})
	case "openai":
		return adkopenai.NewModel(adkopenai.Config{Client: openai.NewClient(openaioption.WithAPIKey("test"), openaioption.WithBaseURL("https://gateway.invalid"), openaioption.WithHTTPClient(client)), Model: cfg})
	default:
		return adkvercel.NewModel(adkvercel.Config{APIKey: "test", HTTPClient: client, Model: cfg})
	}
}
func wireResponse(r *http.Request, adapter string, stream bool) *http.Response {
	body := ""
	contentType := "application/json"
	switch adapter {
	case "anthropic":
		body = `{"id":"msg_1","type":"message","role":"assistant","model":"test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
		if stream {
			body = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + body + "}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		}
	case "openai":
		body = `{"id":"resp_1","model":"test","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
		if stream {
			body = "data: {\"type\":\"response.completed\",\"response\":" + body + "}\n\ndata: [DONE]\n\n"
		}
	default:
		body = `{"content":[{"type":"text","text":"ok"}],"finishReason":{"unified":"stop"},"usage":{"inputTokens":{"total":1},"outputTokens":{"total":1}}}`
		if stream {
			body = "data: {\"type\":\"finish\",\"finishReason\":{\"unified\":\"stop\"},\"usage\":{\"inputTokens\":{\"total\":1},\"outputTokens\":{\"total\":1}}}\n\n"
		}
	}
	if stream {
		contentType = "text/event-stream"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestGatewayForcedToolsSuppressAnthropicThinking(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		t.Run(adapter, func(t *testing.T) {
			var body map[string]any
			client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				return wireResponse(r, adapter, false), nil
			})}
			llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: "claude-test", Vercel: &adkmodels.VercelConfig{}})
			if err != nil {
				t.Fatal(err)
			}
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}, Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "save", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}}}}, ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}}}
			request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("save", genai.RoleUser)}, Config: cfg}
			for _, err := range llm.GenerateContent(t.Context(), request, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
			providers := body["providerOptions"].(map[string]any)
			values := providers["anthropic"].(map[string]any)
			if values["thinking"] != nil || values["effort"] != nil {
				t.Fatalf("forced tools retained reasoning: %#v", values)
			}
			if !reflect.DeepEqual(cfg.ThinkingConfig, &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}) {
				t.Fatal("caller thinking config changed")
			}
		})
	}
}

func TestGatewayPreservesPolicyOnProviderError(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		t.Run(adapter, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				providers := body["providerOptions"].(map[string]any)
				if providers["gateway"].(map[string]any)["zeroDataRetention"] != true {
					t.Fatal("retention requirement weakened")
				}
				return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"retention unavailable"}}`)), Request: r}, nil
			})}
			llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: "gpt-test", Vercel: &adkmodels.VercelConfig{ZeroDataRetention: true}})
			if err != nil {
				t.Fatal(err)
			}
			request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}}
			failed := false
			for _, err := range llm.GenerateContent(t.Context(), request, false) {
				if err != nil {
					failed = true
					if !strings.Contains(err.Error(), "retention unavailable") {
						t.Fatalf("provider error lost: %v", err)
					}
				}
			}
			if !failed || calls != 1 {
				t.Fatalf("failed=%t, calls=%d; expected one rejected request", failed, calls)
			}
		})
	}
}

func TestAdaptersRejectExplicitBudgetsBeforeNetwork(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		for _, budget := range []int32{0, 2048} {
			t.Run(fmt.Sprintf("%s/%d", adapter, budget), func(t *testing.T) {
				client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
					t.Fatal("budget request reached network")
					return nil, nil
				})}
				llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: "gpt-test", Vercel: &adkmodels.VercelConfig{}})
				if err != nil {
					t.Fatal(err)
				}
				request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: &budget}}}
				rejected := false
				for _, err := range llm.GenerateContent(t.Context(), request, false) {
					if err != nil {
						rejected = true
					}
				}
				if !rejected {
					t.Fatal("explicit budget accepted")
				}
			})
		}
	}
}

func TestGatewayConfigurationSnapshot(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		t.Run(adapter, func(t *testing.T) {
			client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				providers := body["providerOptions"].(map[string]any)
				gateway := providers["gateway"].(map[string]any)
				if gateway["zeroDataRetention"] != true || gateway["only"].([]any)[0] != "azure" || providers["custom"].(map[string]any)["flag"] != "original" {
					t.Fatalf("caller mutation changed config: %#v", providers)
				}
				byok, _ := gateway["byok"].(map[string]any)
				credentials, _ := byok["azure"].([]any)
				if len(byok) != 1 || len(credentials) != 2 || credentials[0].(map[string]any)["apiKey"] != "first" || credentials[1].(map[string]any)["apiKey"] != "second" {
					t.Fatal("caller mutation changed BYOK credentials")
				}
				return wireResponse(r, adapter, false), nil
			})}
			cfg := &adkmodels.VercelConfig{ZeroDataRetention: true, Only: []string{"azure"}, ProviderOptions: map[string]map[string]any{"custom": {"flag": "original"}}, BYOK: map[string][]map[string]any{"azure": {{"apiKey": "first"}, {"apiKey": "second"}}}}
			llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: "gpt-test", Vercel: cfg})
			if err != nil {
				t.Fatal(err)
			}
			cfg.ZeroDataRetention = false
			cfg.Only[0] = "changed"
			cfg.ProviderOptions["custom"]["flag"] = "changed"
			cfg.BYOK["azure"][0]["apiKey"] = "changed"
			cfg.BYOK["azure"][1] = map[string]any{"apiKey": "replaced"}
			delete(cfg.BYOK, "azure")
			cfg.BYOK["other"] = []map[string]any{{"apiKey": "added"}}
			request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}}
			for _, err := range llm.GenerateContent(t.Context(), request, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAdaptersIgnoreUnsupportedCacheControls(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		t.Run(adapter, func(t *testing.T) {
			called := false
			client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				for _, field := range []string{"cache_control", "prompt_cache", "promptCache"} {
					if strings.Contains(string(raw), field) {
						t.Errorf("unsupported cache field %q sent: %s", field, raw)
					}
				}
				return wireResponse(r, adapter, false), nil
			})}
			// Gemini has no supported explicit cache boundary in these adapters.
			cfg := adkmodels.ModelConfig{CanonicalModel: "gemini-test", Vercel: &adkmodels.VercelConfig{}, PromptCaching: adkmodels.PromptCachingConfig{
				Anthropic: adkmodels.AnthropicPromptCachingConfig{Mode: adkmodels.AnthropicPromptCacheManual, SystemInstruction: &adkmodels.AnthropicCacheBreakpoint{}, Tools: &adkmodels.AnthropicCacheBreakpoint{}, ConversationHistory: &adkmodels.AnthropicCacheBreakpoint{}},
				OpenAI:    adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheExplicit, Key: "cache-key", SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}, ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}},
			}}
			llm, err := wireModel(adapter, client, cfg)
			if err != nil {
				t.Fatal(err)
			}
			request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText("system", "system")}}
			for _, err := range llm.GenerateContent(t.Context(), request, false) {
				if err != nil {
					t.Fatal(err)
				}
			}
			if !called {
				t.Fatal("request was not sent")
			}
		})
	}
}

func TestGatewayAnthropicThoughtDisplay(t *testing.T) {
	for _, adapter := range []string{"anthropic", "openai", "vercel"} {
		for _, include := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				for _, level := range []genai.ThinkingLevel{"", genai.ThinkingLevelMinimal, genai.ThinkingLevelHigh} {
					t.Run(fmt.Sprintf("%s/include=%t/stream=%t/level=%s", adapter, include, stream, level), func(t *testing.T) {
						client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
							var body map[string]any
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								return nil, err
							}
							options := body["providerOptions"].(map[string]any)
							anthropicOptions, _ := options["anthropic"].(map[string]any)
							thinking, _ := anthropicOptions["thinking"].(map[string]any)
							want := ""
							if level == genai.ThinkingLevelHigh {
								want = "omitted"
								if include {
									want = "summarized"
								}
							}
							assertWireValue(t, thinking, "display", want)
							return wireResponse(r, adapter, stream), nil
						})}
						llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: "claude-test", RequestModel: "anthropic/claude-test", Vercel: &adkmodels.VercelConfig{}})
						if err != nil {
							t.Fatal(err)
						}
						req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: level, IncludeThoughts: include}}}
						for _, err := range llm.GenerateContent(t.Context(), req, stream) {
							if err != nil {
								t.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}
