// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package discovery

import (
	"context"
	"testing"

	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
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
