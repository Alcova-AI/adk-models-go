// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"encoding/json"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/genai"
	"strings"
	"testing"
)

func TestLocalDefinitionsAcrossProviders(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"home":{"$ref":"#/$defs/Address"},"work":{"anyOf":[{"$ref":"#/$defs/Address"},{"type":"null"}]}},"required":["home","work"],"additionalProperties":false,"$defs":{"Address":{"type":"object","properties":{"country":{"type":"string","enum":["AU","NZ"]}},"required":["country"],"additionalProperties":false}}}`)
	for _, target := range []Target{{"openai", "direct"}, {"openai", "vercel-openai"}, {"anthropic", "vertex"}, {"anthropic", "vercel-native"}, {"anthropic", "vercel-anthropic"}, {"google", "vertex"}, {"google", "vercel-native"}, {"google", "vercel-openai"}, {"google", "vercel-anthropic"}} {
		t.Run(target.Provider+"/"+target.Route, func(t *testing.T) {
			result, err := New(Config{AllowUnsupported: true, Warn: quiet}, target).Prepare(t.Context(), tools(rawDeclaration(raw)))
			if err != nil {
				t.Fatal(err)
			}
			encoded := string(result["example"].JSONSchema)
			if strings.Count(encoded, `"$ref":"#/$defs/Address"`) != 2 || !strings.Contains(encoded, `"$defs"`) {
				t.Fatalf("references not preserved: %s", encoded)
			}
			schema := compileTestSchema(t, result["example"].Schema)
			if err := schema.Validate(map[string]any{"home": map[string]any{"country": "AU"}, "work": nil}); err != nil {
				t.Fatal(err)
			}
			if schema.Validate(map[string]any{"home": map[string]any{"country": "XX"}, "work": nil}) == nil {
				t.Fatal("referenced enum lost")
			}
		})
	}
}

func TestUnsupportedReferenceShapesFailWithFallback(t *testing.T) {
	for _, raw := range []string{
		`{"type":"object","properties":{"child":{"$ref":"#"}}}`,
		`{"type":"object","properties":{"child":{"$ref":"#/$defs/A"}},"$defs":{"A":{"$ref":"#/$defs/B"},"B":{"type":"object","properties":{"child":{"$ref":"#/$defs/A"}}}}}`,
		`{"type":"object","properties":{"child":{"$ref":"#/$defs/A"}},"$defs":{"A":{"$id":"https://example.invalid/inner","type":"object"}}}`,
	} {
		for _, provider := range []string{"openai", "anthropic", "google"} {
			if _, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{provider, "direct"}).Prepare(t.Context(), tools(rawDeclaration(json.RawMessage(raw)))); err == nil {
				t.Fatalf("accepted unsupported reference on %s: %s", provider, raw)
			}
		}
	}
	raw := json.RawMessage(`{"type":"object","properties":{"value":{"allOf":[{"$ref":"#/$defs/A"}]}},"$defs":{"A":{"type":"string"}}}`)
	if _, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"anthropic", "vertex"}).Prepare(t.Context(), tools(rawDeclaration(raw))); err == nil {
		t.Fatal("accepted allOf with a reference on Claude")
	}
}

func rawDeclaration(raw json.RawMessage) *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{Name: "example", ParametersJsonSchema: raw}
}

func compileTestSchema(t *testing.T, schema map[string]any) *validator.Schema {
	t.Helper()
	compiler := validator.NewCompiler()
	if err := compiler.AddResource("urn:test", schema); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("urn:test")
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}
