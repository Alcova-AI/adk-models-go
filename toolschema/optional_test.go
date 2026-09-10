// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"encoding/json"
	"iter"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestResponsesOmissionRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"first":{"type":"integer"},"last":{"type":"integer"},"explicit":{"anyOf":[{"type":"integer"},{"type":"null"}]},"required":{"type":"integer"},"rows":{"type":"array","items":{"type":"object","properties":{"value":{"type":"integer"}}}}},"required":["required"]}`)
	fd := &genai.FunctionDeclaration{Name: "example", ParametersJsonSchema: raw}
	target := Target{"openai", "vercel-openai"}
	if _, err := New(Config{}, target).Prepare(t.Context(), tools(fd)); err == nil {
		t.Fatal("omission encoding requires opt-in")
	}
	prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, target).Prepare(t.Context(), tools(fd))
	if err != nil {
		t.Fatal(err)
	}
	props := prepared["example"].Schema["properties"].(map[string]any)
	if props["last"].(map[string]any)["anyOf"] == nil {
		t.Fatal("missing omission encoding")
	}
	if props["required"].(map[string]any)["anyOf"] != nil {
		t.Fatal("required field widened")
	}
	call := &genai.FunctionCall{Name: "example", Args: map[string]any{"first": float64(5), "last": nil, "explicit": nil, "required": float64(0), "rows": []any{map[string]any{"value": nil}, map[string]any{"value": float64(0)}}}}
	original := &model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: call}}}}

	seq := iter.Seq2[*model.LLMResponse, error](func(yield func(*model.LLMResponse, error) bool) { yield(original, nil) })
	for response, err := range RestoreOmissions(seq, prepared) {
		if err != nil {
			t.Fatal(err)
		}
		args := response.Content.Parts[0].FunctionCall.Args
		if _, ok := args["last"]; ok {
			t.Fatal("synthetic null retained")
		}
		if _, ok := args["explicit"]; !ok {
			t.Fatal("real null removed")
		}
		if args["required"] != float64(0) {
			t.Fatal("zero changed")
		}
		rows := args["rows"].([]any)
		if len(rows[0].(map[string]any)) != 0 || rows[1].(map[string]any)["value"] != float64(0) {
			t.Fatal("nested omissions not restored")
		}
	}
	if _, ok := original.Content.Parts[0].FunctionCall.Args["last"]; !ok {
		t.Fatal("provider response mutated")
	}
	if string(fd.ParametersJsonSchema.(json.RawMessage)) != string(raw) {
		t.Fatal("input schema mutated")
	}
	original.Partial = true
	for response, _ := range RestoreOmissions(seq, prepared) {
		if _, ok := response.Content.Parts[0].FunctionCall.Args["last"]; !ok {
			t.Fatal("partial changed")
		}
	}
}

func TestOtherRoutesDoNotEncodeOmissions(t *testing.T) {
	for _, target := range []Target{{"openai", "direct"}, {"openai", "vercel-native"}, {"anthropic", "vercel-anthropic"}, {"google", "vertex"}} {
		prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, target).Prepare(t.Context(), tools(&genai.FunctionDeclaration{Name: "example", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"last": map[string]any{"type": "integer"}}}}))
		if err != nil {
			t.Fatal(err)
		}
		if prepared["example"].omissions != nil {
			t.Fatalf("unexpected omission conversion: %v", target)
		}
	}
}

func TestResponsesReferencedOmissionsKeepRequiredAndRealNulls(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"required_item":{"$ref":"#/$defs/Item"},"optional_item":{"$ref":"#/$defs/Alias"},"nullable_item":{"anyOf":[{"$ref":"#/$defs/Item"},{"type":"null"}]},"rows":{"type":"array","items":{"$ref":"#/$defs/Item"}}},"required":["required_item","nullable_item","rows"],"$defs":{"Alias":{"$ref":"#/$defs/Item"},"Item":{"type":"object","properties":{"count":{"type":"integer"},"explicit":{"anyOf":[{"type":"integer"},{"type":"null"}]}},"required":["explicit"]}}}`)
	prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"openai", "vercel-openai"}).Prepare(t.Context(), tools(rawDeclaration(raw)))
	if err != nil {
		t.Fatal(err)
	}
	schema := prepared["example"].Schema
	validator := compileTestSchema(t, schema)
	input := map[string]any{
		"required_item": map[string]any{"count": float64(0), "explicit": nil},
		"optional_item": nil,
		"nullable_item": map[string]any{"count": nil, "explicit": nil},
		"rows":          []any{map[string]any{"count": nil, "explicit": nil}},
	}
	if err := validator.Validate(input); err != nil {
		t.Fatal(err)
	}
	result := prepared["example"].omissions.restore(input).(map[string]any)
	if _, ok := result["optional_item"]; ok {
		t.Fatal("optional reference null not restored to omission")
	}
	if result["required_item"].(map[string]any)["count"] != float64(0) {
		t.Fatal("required shared definition or zero changed")
	}
	for _, item := range []any{result["nullable_item"], result["rows"].([]any)[0]} {
		row := item.(map[string]any)
		if _, ok := row["count"]; ok {
			t.Fatal("nested reference omission not restored")
		}
		if v, ok := row["explicit"]; !ok || v != nil {
			t.Fatal("legitimate null removed")
		}
	}
	input["nullable_item"] = nil
	result = prepared["example"].omissions.restore(input).(map[string]any)
	if v, ok := result["nullable_item"]; !ok || v != nil {
		t.Fatal("outer nullable reference changed")
	}
	input["required_item"] = nil
	if validator.Validate(input) == nil {
		t.Fatal("optional reference widened the required use of the definition")
	}
	result = prepared["example"].omissions.restore(input).(map[string]any)
	if _, ok := result["required_item"]; !ok {
		t.Fatal("required reference was treated as optional")
	}
}
