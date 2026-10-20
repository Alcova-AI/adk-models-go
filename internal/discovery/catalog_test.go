// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package discovery

import (
	"context"
	"testing"

	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestNativeCatalogueStableWithoutMutatingCaller(t *testing.T) {
	p, req := fixture(t, "anthropic", false)
	ctx := tooldiscovery.WithCatalog(context.Background(), p.Catalog)
	out := CanonicalRequest(ctx, tooldiscovery.Native, req)
	if out == req || out.Config == req.Config || req.Config.Tools[0].FunctionDeclarations[0].Name != "search" || out.Config.Tools[0].FunctionDeclarations[0].Name != "lookup" {
		t.Fatal("unexpected alias/order")
	}
	if CanonicalRequest(ctx, tooldiscovery.Application, req) != req {
		t.Fatal("opt-out changed request")
	}
}

func TestInheritedCatalogueWithoutFunctionsLeavesRequestUnchanged(t *testing.T) {
	for _, config := range []*genai.GenerateContentConfig{nil, {}, {Tools: []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}}} {
		req := &model.LLMRequest{Config: config}
		ctx := tooldiscovery.WithCatalog(t.Context(), tooldiscovery.Catalog{SearchTool: "search"})
		if got := CanonicalRequest(ctx, tooldiscovery.Native, req); got != req {
			t.Fatal("request without functions was rewritten")
		}
	}
}
