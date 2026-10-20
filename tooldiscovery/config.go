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
// All authorised declarations, including deferred ones, must be present in
// LLMRequest.Config.Tools. The application remains responsible for restricting
// executable LLMRequest.Tools to loaded tools. Keep the catalogue and AlwaysLoaded
// set stable while authorisation is unchanged; adapters canonicalise the order.
// Calls without catalogue context retain ordinary tool calling. Native OpenAI
// discovery rejects named tool-choice restrictions on search or deferred tools;
// ordinary tool loading preserves those restrictions.
//
// Persist genai.Part.PartMetadata with conversation history: it carries native
// discovery IDs and namespaces for replay. If discovery history is unavailable,
// including after compaction or application-side loading, adapters use ordinary
// loaded tools with a warning. The same fallback applies when a saved search
// result refers to a tool missing from the current authorised catalogue; removed
// definitions are never restored. This can cause a cache miss. Catalogue and session
// data are request-local and are not retained on the shared model instance.
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
