// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package tooldiscovery supplies caller-owned discovery metadata. It does not
// search a catalogue, load tools, authorise calls or execute functions.
package tooldiscovery

import (
	"context"
	"slices"
)

type Mode string

const (
	Application Mode = ""
	Native      Mode = "native"
)

// Catalog describes the application's existing search contract. ResolveResult
// returns exact names loaded by a search result; it must not perform I/O.
// All declarations, including deferred ones, must be present in LLMRequest.Config.
// The application remains responsible for filtering executable LLMRequest.Tools.
type Catalog struct {
	SearchTool    string
	AlwaysLoaded  []string
	ResolveResult func(map[string]any) ([]string, error)
}
type catalogKey struct{}

func WithCatalog(ctx context.Context, c Catalog) context.Context {
	c.AlwaysLoaded = slices.Clone(c.AlwaysLoaded)
	return context.WithValue(ctx, catalogKey{}, c)
}
func FromContext(ctx context.Context) (Catalog, bool) {
	c, ok := ctx.Value(catalogKey{}).(Catalog)
	c.AlwaysLoaded = slices.Clone(c.AlwaysLoaded)
	return c, ok
}
