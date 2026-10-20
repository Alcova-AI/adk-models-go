// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkmodels_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/discovery"
	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestNativeDiscoveryReviewRegressions(t *testing.T) {
	for _, adapter := range []string{"openai", "vercel"} {
		for _, stream := range []bool{false, true} {
			for _, scenario := range []string{"tool-free", "forced-search", "restricted-auto-search", "forced-deferred", "forced-always", "disabled-search", "fallback-forced-search", "missing-metadata", "partial-metadata", "wrong-namespace", "wrong-provider", "valid-metadata"} {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", adapter, stream, scenario), func(t *testing.T) {
					schema := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
					req := &model.LLMRequest{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "search", ParametersJsonSchema: schema}, {Name: "lookup", ParametersJsonSchema: schema}, {Name: "always", ParametersJsonSchema: schema}}}}}, Contents: []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)}}
					catalog := tooldiscovery.Catalog{SearchTool: "search", AlwaysLoaded: []string{"always"}, ResolveResult: func(map[string]any) ([]string, error) { return []string{"lookup"}, nil }}
					wantError := false
					fallback := false
					switch scenario {
					case "tool-free":
						req.Config.Tools = nil
					case "forced-search", "restricted-auto-search", "forced-deferred", "forced-always", "disabled-search", "fallback-forced-search":
						name := "search"
						mode := genai.FunctionCallingConfigModeAny
						if scenario == "restricted-auto-search" {
							mode = genai.FunctionCallingConfigModeAuto
						}
						if scenario == "forced-deferred" {
							name = "lookup"
						}
						if scenario == "forced-always" {
							name = "always"
						}
						if scenario == "disabled-search" {
							mode = genai.FunctionCallingConfigModeNone
						}
						if scenario == "fallback-forced-search" {
							req.Tools = map[string]any{"lookup": struct{}{}}
							fallback = true
						}
						req.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: mode, AllowedFunctionNames: []string{name}}}
						wantError = scenario == "forced-search" || scenario == "restricted-auto-search" || scenario == "forced-deferred"
					default:
						namespace := (&discovery.Plan{}).Namespace("search-call")
						call := &genai.Part{FunctionCall: &genai.FunctionCall{ID: "lookup-call", Name: "lookup", Args: map[string]any{}}}
						req.Contents = append(req.Contents,
							&genai.Content{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "search-call", Name: "search", Args: map[string]any{}}}}},
							&genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "search-call", Name: "search", Response: map[string]any{"status": "loaded"}}}}},
							&genai.Content{Role: "model", Parts: []*genai.Part{call}},
							&genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "lookup-call", Name: "lookup", Response: map[string]any{"answer": "saved"}}}}})
						if scenario != "missing-metadata" {
							call.PartMetadata = map[string]any{discovery.MetadataKey: map[string]any{"provider": "openai", "namespace": namespace}}
						}
						if scenario == "wrong-namespace" {
							call.PartMetadata = map[string]any{discovery.MetadataKey: map[string]any{"provider": "openai", "namespace": "other_namespace"}}
						}
						if scenario == "wrong-provider" {
							call.PartMetadata = map[string]any{discovery.MetadataKey: map[string]any{"provider": "anthropic", "namespace": namespace}}
						}
						if scenario == "partial-metadata" {
							req.Contents = append(req.Contents, &genai.Content{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "second-lookup", Name: "lookup", Args: map[string]any{}}}}}, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "second-lookup", Name: "lookup", Response: map[string]any{"answer": "saved again"}}}}})
						}
						fallback = scenario != "valid-metadata"
					}
					before, err := json.Marshal(req)
					if err != nil {
						t.Fatal(err)
					}
					var body map[string]any
					calls := 0
					client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							return nil, err
						}
						return wireResponse(r, adapter, stream), nil
					})}
					llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: "gpt-test", RequestModel: "openai/gpt-test", ToolDiscovery: tooldiscovery.Native, Vercel: &adkmodels.VercelConfig{}})
					if err != nil {
						t.Fatal(err)
					}
					ctx := tooldiscovery.WithCatalog(t.Context(), catalog)
					var generateErr error
					for _, err := range llm.GenerateContent(ctx, req, stream) {
						if err != nil {
							generateErr = err
							break
						}
					}
					after, err := json.Marshal(req)
					if err != nil {
						t.Fatal(err)
					}
					if string(before) != string(after) {
						t.Fatal("caller request mutated")
					}
					if wantError {
						if generateErr == nil || !strings.Contains(generateErr.Error(), "named tool-choice") {
							t.Fatalf("want local named tool-choice error, got %v", generateErr)
						}
						if calls != 0 {
							t.Fatalf("sent incompatible request: %d calls", calls)
						}
						return
					}
					if generateErr != nil {
						t.Fatal(generateErr)
					}
					if calls != 1 {
						t.Fatalf("HTTP calls=%d, want 1", calls)
					}
					tools, _ := body["tools"].([]any)
					if scenario == "tool-free" {
						if len(tools) != 0 {
							t.Fatal("tool-free helper acquired tools", tools)
						}
						return
					}
					if fallback {
						for _, raw := range tools {
							tool := raw.(map[string]any)
							if tool["type"] != "function" {
								t.Fatal("fallback retained native discovery", tool)
							}
						}
						if scenario != "fallback-forced-search" {
							found := false
							for _, raw := range tools {
								if raw.(map[string]any)["name"] == "lookup" {
									found = true
								}
							}
							if !found {
								t.Fatal("fallback hid loaded lookup")
							}
							data, _ := json.Marshal(body)
							text := string(data)
							if strings.Contains(text, "adk_discovery_") || !strings.Contains(text, "lookup-call") || !strings.Contains(text, "saved") {
								t.Fatal("fallback lost ordinary replay", text)
							}
						}
					}
					if scenario == "forced-always" || scenario == "fallback-forced-search" || scenario == "disabled-search" {
						choiceKey := "tool_choice"
						if adapter == "vercel" {
							choiceKey = "toolChoice"
						}
						if scenario == "disabled-search" && adapter == "openai" {
							if body[choiceKey] != "none" {
								t.Fatal("disabled tools enabled", body[choiceKey])
							}
							return
						}
						choice, ok := body[choiceKey].(map[string]any)
						if !ok {
							t.Fatal("missing tool-choice restriction", body)
						}
						if scenario == "disabled-search" {
							if adapter == "vercel" && choice["type"] != "none" {
								t.Fatal("disabled tools enabled", choice)
							}
						} else if adapter == "vercel" {
							if choice["type"] != "required" || len(tools) != 1 {
								t.Fatal("forced tool restriction lost", body)
							}
						} else {
							if choice["type"] != "allowed_tools" || choice["mode"] != "required" {
								t.Fatal("forced tool restriction lost", choice)
							}
						}
					}
					if scenario == "valid-metadata" {
						data, _ := json.Marshal(body)
						if !strings.Contains(string(data), (&discovery.Plan{}).Namespace("search-call")) {
							t.Fatal("valid namespace lost", string(data))
						}
					}
				})
			}
		}
	}
}
