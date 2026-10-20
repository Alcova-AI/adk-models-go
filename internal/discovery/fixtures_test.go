// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package discovery

import (
	"context"
	"testing"

	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func fixture(t *testing.T, provider string, empty bool) (*Plan, *model.LLMRequest) {
	t.Helper()
	schema := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "search", ParametersJsonSchema: schema}, {Name: "lookup", ParametersJsonSchema: schema}}}}}, Contents: []*genai.Content{{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "call1", Name: "search", Args: map[string]any{"query": "lookup"}}}}}, {Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "call1", Name: "search", Response: map[string]any{"status": "loaded", "feedback": "keep this"}}}}}}}
	ctx := tooldiscovery.WithCatalog(context.Background(), tooldiscovery.Catalog{SearchTool: "search", ResolveResult: func(map[string]any) ([]string, error) {
		if empty {
			return nil, nil
		}
		return []string{"lookup"}, nil
	}})
	prep, err := toolschema.New(toolschema.Config{}, toolschema.Target{Provider: provider, Route: "vercel-native"}).Prepare(ctx, toolschema.Tools(req))
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(ctx, tooldiscovery.Native, req, prep, provider)
	if err != nil {
		t.Fatal(err)
	}
	return p, req
}
