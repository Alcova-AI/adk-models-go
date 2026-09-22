// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"encoding/json"
	"testing"

	"google.golang.org/genai"
)

func TestGenericGatewayStillValidatesSchemas(t *testing.T) {
	for _, raw := range []string{
		`{"type":"object","properties":{"n":{"type":"integer","minimum":"invalid"}}}`,
		`{"type":"object","unknownKeyword":true}`,
		`{"type":"object","properties":{"n":{"$ref":"https://example.com/schema"}}}`,
		`{"type":"object","properties":{"n":{"$ref":"#/$defs/missing"}}}`,
		`{"type":"object","properties":{"n":{"$anchor":"n","type":"string"}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			p := New(Config{}, Target{"gateway", "vercel-native"})
			if _, err := p.Prepare(t.Context(), tools(&genai.FunctionDeclaration{Name: "lookup", ParametersJsonSchema: json.RawMessage(raw)})); err == nil {
				t.Fatal("invalid schema accepted")
			}
		})
	}
	if _, err := New(Config{}, Target{"gateway", "direct"}).Prepare(t.Context(), tools(&genai.FunctionDeclaration{Name: "lookup"})); err == nil {
		t.Fatal("generic profile accepted outside native Gateway")
	}
}
