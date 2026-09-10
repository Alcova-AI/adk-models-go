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
