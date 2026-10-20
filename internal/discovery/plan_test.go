// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package discovery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
)

func TestEmptySearchRetainsNativeDiscovery(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		p, _ := fixture(t, provider, true)
		if p.Fallback {
			t.Fatal(provider)
		}
	}
}

func TestUnavailableDiscoveryNameIsNotRestored(t *testing.T) {
	p, req := fixture(t, "openai", false)
	p.Catalog.ResolveResult = func(map[string]any) ([]string, error) { return []string{"unauthorised"}, nil }
	ctx := tooldiscovery.WithCatalog(context.Background(), p.Catalog)
	plan, err := New(ctx, tooldiscovery.Native, req, p.Prepared, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Fallback || plan.Loaded("unauthorised") || len(plan.Names["call1"]) != 0 {
		t.Fatal("unavailable name was restored or native replay was retained")
	}
}

func TestLoadedToolsWithoutSearchHistoryRemainVisible(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			previous, req := fixture(t, provider, false)
			req.Contents = nil
			req.Tools = map[string]any{"search": struct{}{}, "lookup": struct{}{}}
			before, _ := json.Marshal(req)
			ctx := tooldiscovery.WithCatalog(context.Background(), previous.Catalog)
			p, err := New(ctx, tooldiscovery.Native, req, previous.Prepared, provider)
			if err != nil {
				t.Fatal(err)
			}
			if !p.Fallback {
				t.Fatal("loaded tool was left deferred without discovery history")
			}
			body := map[string]any{"tools": []any{map[string]any{"name": "search"}, map[string]any{"name": "lookup"}, map[string]any{"name": "unloaded"}}}
			if provider == "anthropic" {
				p.AnthropicWire(body)
			} else if err := p.OpenAIWire(body); err != nil {
				t.Fatal(err)
			}
			tools := objects(body["tools"])
			if len(tools) != 2 || text(tools[1]["name"]) != "lookup" || tools[1]["defer_loading"] != nil {
				t.Fatal("loaded tools not exposed ordinarily", tools)
			}
			o := protocol.CallOptions{Tools: []protocol.FunctionTool{{Type: "function", Name: "search"}, {Type: "function", Name: "lookup"}, {Type: "function", Name: "unloaded"}}}
			if err := p.VercelWire(&o); err != nil {
				t.Fatal(err)
			}
			if len(o.Tools) != 2 || o.Tools[1].Name != "lookup" || o.Tools[1].ProviderOptions != nil {
				t.Fatal("gateway loaded tools not exposed ordinarily", o.Tools)
			}
			after, _ := json.Marshal(req)
			if string(before) != string(after) {
				t.Fatal("request mutated")
			}
		})
	}
}
