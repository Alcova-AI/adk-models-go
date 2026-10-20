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

func TestNativeDiscoveryAfterToolRemoval(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "vercel", "vercel-anthropic"} {
		for _, stream := range []bool{false, true} {
			for _, retainLookup := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%t/retained=%t", adapter, stream, retainLookup), func(t *testing.T) {
					schema := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
					declarations := []*genai.FunctionDeclaration{}
					for _, name := range []string{"search", "always", "lookup", "unloaded"} {
						declarations = append(declarations, &genai.FunctionDeclaration{Name: name, ParametersJsonSchema: schema})
					}
					names := []any{"removed"}
					executable := map[string]any{"search": struct{}{}, "always": struct{}{}}
					if retainLookup {
						names = append(names, "lookup")
						executable["lookup"] = struct{}{}
					}
					provider := "openai"
					canonical := "gpt-test"
					if strings.Contains(adapter, "anthropic") {
						provider = "anthropic"
						canonical = "claude-test"
					}
					req := &model.LLMRequest{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: declarations}}}, Tools: executable, Contents: []*genai.Content{
						{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "search-call", Name: "search", Args: map[string]any{}}}}},
						{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "search-call", Name: "search", Response: map[string]any{"loaded": names}}}}},
						{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "removed-call", Name: "removed", Args: map[string]any{}}, PartMetadata: map[string]any{discovery.MetadataKey: map[string]any{"provider": provider, "namespace": (&discovery.Plan{}).Namespace("search-call")}}}}},
						{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "removed-call", Name: "removed", Response: map[string]any{"saved": "previous result"}}}}},
					}}
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
					llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: canonical, RequestModel: provider + "/" + canonical, ToolDiscovery: tooldiscovery.Native, Vercel: &adkmodels.VercelConfig{}})
					if err != nil {
						t.Fatal(err)
					}
					ctx := tooldiscovery.WithCatalog(t.Context(), tooldiscovery.Catalog{SearchTool: "search", AlwaysLoaded: []string{"always"}, ResolveResult: func(result map[string]any) ([]string, error) {
						loaded := []string{}
						for _, raw := range result["loaded"].([]any) {
							loaded = append(loaded, raw.(string))
						}
						return loaded, nil
					}})
					for _, err := range llm.GenerateContent(ctx, req, stream) {
						if err != nil {
							t.Fatal(err)
						}
					}
					if calls != 1 {
						t.Fatalf("HTTP calls=%d, want 1", calls)
					}
					tools, ok := body["tools"].([]any)
					if !ok {
						t.Fatal("missing tools", body)
					}
					want := map[string]bool{"search": true, "always": true}
					if retainLookup {
						want["lookup"] = true
					}
					if len(tools) != len(want) {
						t.Fatalf("tool count=%d, want %d: %v", len(tools), len(want), tools)
					}
					for _, raw := range tools {
						tool := raw.(map[string]any)
						name, _ := tool["name"].(string)
						if !want[name] {
							t.Fatal("exposed unavailable or unloaded tool", tool)
						}
						delete(want, name)
					}
					if len(want) != 0 {
						t.Fatal("loaded tools missing", want)
					}
					data, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					text := string(data)
					for _, native := range []string{"adk_discovery_", "openai.tool_search", "defer_loading", "deferLoading"} {
						if strings.Contains(text, native) {
							t.Fatalf("fallback retained native marker %q: %s", native, text)
						}
					}
					if !strings.Contains(text, "removed-call") || !strings.Contains(text, "previous result") || !strings.Contains(text, "search-call") {
						t.Fatal("saved history lost", text)
					}
					after, err := json.Marshal(req)
					if err != nil {
						t.Fatal(err)
					}
					if string(before) != string(after) {
						t.Fatal("caller request or dispatch map mutated")
					}
				})
			}
		}
	}
}
