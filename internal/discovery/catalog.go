// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package discovery

import (
	"context"
	"slices"
	"strings"

	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// CanonicalRequest stabilises native catalogue order before strict allocation
// and cache markers. It never mutates the caller's request or dispatch map.
func CanonicalRequest(ctx context.Context, mode tooldiscovery.Mode, req *model.LLMRequest) *model.LLMRequest {
	if mode != tooldiscovery.Native || req == nil || req.Config == nil {
		return req
	}
	if _, ok := tooldiscovery.FromContext(ctx); !ok {
		return req
	}
	out := *req
	cfg := *req.Config
	out.Config = &cfg
	declarations := []*genai.FunctionDeclaration{}
	other := []*genai.Tool{}
	for _, t := range cfg.Tools {
		if t == nil {
			return req
		}
		if len(t.FunctionDeclarations) == 0 {
			other = append(other, t)
			continue
		}
		declarations = append(declarations, t.FunctionDeclarations...)
	}
	if slices.ContainsFunc(declarations, func(d *genai.FunctionDeclaration) bool { return d == nil }) {
		return req
	}
	slices.SortFunc(declarations, func(a, b *genai.FunctionDeclaration) int { return strings.Compare(a.Name, b.Name) })
	cfg.Tools = append(other, &genai.Tool{FunctionDeclarations: declarations})
	return &out
}
