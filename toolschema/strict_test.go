// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package toolschema

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"google.golang.org/genai"
)

func TestOpenAIStrictOptionalRoundTrip(t *testing.T) {
	for _, route := range []string{"direct", "vercel-openai", "vercel-native", "chat", "vercel-openai-chat"} {
		t.Run(route, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"},"count":{"type":"integer"},"enabled":{"type":"boolean"}},"required":["label"]}`)
			prepared, err := New(Config{}, Target{"openai", route}).Prepare(t.Context(), tools(rawDeclaration(raw)))
			if err != nil {
				t.Fatal(err)
			}
			got := prepared["example"]
			if got.Strict == nil || !*got.Strict {
				t.Fatal("compatible tool did not enable strict mode")
			}
			v := compileTestSchema(t, got.Schema)
			valid := map[string]any{"label": "example", "count": nil, "enabled": false}
			if err := v.Validate(valid); err != nil {
				t.Fatal(err)
			}
			restored := got.omissions.restore(valid).(map[string]any)
			if _, ok := restored["count"]; ok {
				t.Fatal("omitted count retained")
			}
			if restored["enabled"] != false {
				t.Fatal("explicit false lost")
			}
			valid["count"] = float64(0)
			if got.omissions.restore(valid).(map[string]any)["count"] != float64(0) {
				t.Fatal("explicit zero lost")
			}
			for _, bad := range []map[string]any{{"label": "example", "enabled": false}, {"label": "example", "count": "wrong", "enabled": false}, {"label": "example", "count": nil, "enabled": false, "extra": true}} {
				if v.Validate(bad) == nil {
					t.Fatalf("accepted invalid wire input: %v", bad)
				}
			}
		})
	}
}

func TestOptionalNullableValuesKeepNonStrictSemantics(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"patch":{"type":["string","null"]}}}`)
	prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"openai", "vercel-native"}).Prepare(t.Context(), tools(rawDeclaration(raw)))
	if err != nil {
		t.Fatal(err)
	}
	got := prepared["example"]
	if got.Strict == nil || *got.Strict {
		t.Fatal("ambiguous optional nullable field must remain non-strict")
	}
	for _, input := range []map[string]any{{}, {"patch": nil}, {"patch": ""}} {
		output := got.omissions.restore(input).(map[string]any)
		if _, ok := output["patch"]; ok != (len(input) == 1) {
			t.Fatal("null/omission distinction lost")
		}
	}
}

func TestAnthropicStrictBudgetIsRequestWide(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		tools, optional, unions, strict int
	}{{"tool count", 21, 0, 0, 20}, {"optional budget", 5, 6, 0, 4}, {"union budget", 3, 0, 8, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			declarations := []*genai.FunctionDeclaration{}
			for i := 0; i < tc.tools; i++ {
				props := map[string]any{}
				required := []any{}
				for j := 0; j < tc.optional; j++ {
					props[fmt.Sprint("optional", j)] = map[string]any{"type": "string"}
				}
				for j := 0; j < tc.unions; j++ {
					name := fmt.Sprint("union", j)
					props[name] = map[string]any{"type": []any{"string", "null"}}
					required = append(required, name)
				}
				declarations = append(declarations, &genai.FunctionDeclaration{Name: fmt.Sprint("tool", i), ParametersJsonSchema: map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}})
			}
			p := New(Config{Warn: quiet}, Target{"anthropic", "vercel-anthropic"})
			for range 2 {
				result, err := p.Prepare(t.Context(), []*genai.Tool{{FunctionDeclarations: declarations}})
				if err != nil {
					t.Fatal(err)
				}
				strict := 0
				for _, tool := range result {
					if tool.Strict != nil && *tool.Strict {
						strict++
					}
				}
				if strict != tc.strict {
					t.Fatalf("strict=%d want%d", strict, tc.strict)
				}
			}
		})
	}
}

func TestAnthropicKeepsSmallOptionalToolStrict(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"}}}`)
	result, err := New(Config{}, Target{"anthropic", "vercel-anthropic"}).Prepare(t.Context(), tools(rawDeclaration(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if !*result["example"].Strict {
		t.Fatal("small optional tool must remain strict")
	}
	if err := compileTestSchema(t, result["example"].Schema).Validate(map[string]any{}); err != nil {
		t.Fatal("optional value became required", err)
	}
}

func TestAnthropicStrictSelectionUsesCallerPriority(t *testing.T) {
	declarations := []*genai.FunctionDeclaration{}
	for i := 0; i <= 20; i++ {
		optional := 1
		if i == 0 {
			optional = 22
		}
		properties := map[string]any{}
		for j := 0; j < optional; j++ {
			properties[fmt.Sprintf("field%d", j)] = map[string]any{"type": "string"}
		}
		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:                 fmt.Sprintf("tool%02d", i),
			ParametersJsonSchema: map[string]any{"type": "object", "properties": properties, "additionalProperties": false},
		})
	}
	processor := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"anthropic", "vercel-anthropic"})
	for _, reverse := range []bool{false, true} {
		ordered := append([]*genai.FunctionDeclaration(nil), declarations...)
		if reverse {
			for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
		priority := []string{"not_available", "tool01", "tool01"}
		for i := 2; i <= 20; i++ {
			priority = append(priority, fmt.Sprintf("tool%02d", i))
		}
		processor = New(Config{StrictModeHints: priority, Warn: quiet}, Target{"anthropic", "vercel-anthropic"})
		ctx := t.Context()
		result, err := processor.Prepare(ctx, []*genai.Tool{{FunctionDeclarations: ordered[:10]}, {FunctionDeclarations: ordered[10:]}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != len(declarations) {
			t.Fatal("selection removed a tool")
		}
		for i, declaration := range declarations {
			prepared := result[declaration.Name]
			if prepared.Strict == nil || *prepared.Strict != (i != 0) {
				t.Fatalf("reverse=%v tool=%s strict=%v; want the twenty caller-prioritised tools strict", reverse, declaration.Name, prepared.Strict)
			}
			validator := compileTestSchema(t, prepared.Schema)
			if err := validator.Validate(map[string]any{}); err != nil {
				t.Fatal("selection changed optional-field semantics", err)
			}
			if err := validator.Validate(map[string]any{"field0": ""}); err != nil {
				t.Fatal("selection changed empty-value semantics", err)
			}
		}
	}
}

func TestStrictSelectionReportsEachRequestAndKeepsPriorityOverSchemaSize(t *testing.T) {
	large := map[string]any{}
	for i := 0; i < 22; i++ {
		large[fmt.Sprintf("optional%d", i)] = map[string]any{"type": "string"}
	}
	declarations := []*genai.FunctionDeclaration{
		{Name: "small", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "additionalProperties": false}},
		{Name: "large", ParametersJsonSchema: map[string]any{"type": "object", "properties": large, "additionalProperties": false}},
		{Name: "small2", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "additionalProperties": false}},
		{Name: "small3", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "additionalProperties": false}},
	}
	var selections []StrictSelection
	p := New(Config{AllowUnsupported: true, Warn: quiet, OnStrictSelection: func(_ context.Context, s StrictSelection) { selections = append(selections, s) }}, Target{"anthropic", "vercel-anthropic"})
	names := []string{"large", "small", "small2", "small3"}
	p.config.StrictModeHints = append([]string(nil), names...)
	ctx := t.Context()
	names[0] = "small3"
	for range 2 {
		result, err := p.Prepare(ctx, []*genai.Tool{{FunctionDeclarations: declarations}})
		if err != nil {
			t.Fatal(err)
		}
		if !*result["large"].Strict || *result["small3"].Strict {
			t.Fatal("explicit priority did not take precedence")
		}
	}
	if len(selections) != 2 {
		t.Fatal("decisions must be reported every request")
	}
	for _, s := range selections {
		if s.Provider != "anthropic" || s.Route != "vercel-anthropic" || len(s.Tools) != 4 {
			t.Fatal("incomplete decision report")
		}
		if s.Tools[1].Tool != "large" || !s.Tools[1].Hinted || !s.Tools[1].Strict {
			t.Fatal("priority decision missing")
		}
		if s.Tools[3].Strict || s.Tools[3].Reason != "request_budget" {
			t.Fatal("budget reason missing")
		}
	}
}

func TestStrictModeHintsPresenceAndDefensiveCopy(t *testing.T) {
	declarations := []*genai.FunctionDeclaration{}
	for _, name := range []string{"a", "b"} {
		declarations = append(declarations, &genai.FunctionDeclaration{Name: name, ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}})
	}
	for _, provider := range []string{"openai", "anthropic"} {
		for _, tc := range []struct {
			name         string
			hints        []string
			wantA, wantB bool
		}{{"nil", nil, true, true}, {"empty", []string{}, false, false}, {"explicit", []string{"absent", "b", "b"}, false, true}} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				p := New(Config{StrictModeHints: tc.hints, Warn: quiet}, Target{provider, "direct"})
				if len(tc.hints) > 0 {
					tc.hints[1] = "a"
				}
				got, err := p.Prepare(t.Context(), []*genai.Tool{{FunctionDeclarations: declarations}})
				if err != nil {
					t.Fatal(err)
				}
				wantA, wantB := tc.wantA, tc.wantB
				if provider == "openai" {
					wantA, wantB = true, true
				}
				if *got["a"].Strict != wantA || *got["b"].Strict != wantB {
					t.Fatalf("wrong selection: %+v", got)
				}
			})
		}
	}
}

func TestOpenAIIgnoresHintsWithoutChangingSchemasOrDiagnostics(t *testing.T) {
	declarations := []*genai.FunctionDeclaration{
		{Name: "compatible", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{Name: "open_record", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
	}
	for _, route := range []string{"direct", "vercel-openai", "vercel-native", "chat", "vercel-openai-chat"} {
		t.Run(route, func(t *testing.T) {
			var baseline string
			for _, hints := range [][]string{nil, {}, {"absent"}, {"compatible"}} {
				warnings := []Warning{}
				var selection StrictSelection
				cfg := Config{StrictModeHints: hints, Warn: func(_ context.Context, w Warning) { warnings = append(warnings, w) }, OnStrictSelection: func(_ context.Context, s StrictSelection) { selection = s }}
				got, err := New(cfg, Target{"openai", route}).Prepare(t.Context(), []*genai.Tool{{FunctionDeclarations: declarations}})
				if err != nil {
					t.Fatal(err)
				}
				if !*got["compatible"].Strict || *got["open_record"].Strict {
					t.Fatal("hints changed compatibility selection")
				}
				data, err := json.Marshal(struct {
					Prepared  map[string]Prepared
					Warnings  []Warning
					Selection StrictSelection
				}{got, warnings, selection})
				if err != nil {
					t.Fatal(err)
				}
				if baseline == "" {
					baseline = string(data)
				} else if string(data) != baseline {
					t.Fatal("hints changed OpenAI schemas, strict flags or diagnostics")
				}
				if len(warnings) != 1 || warnings[0].Tool != "open_record" {
					t.Fatal("incompatibility warning lost", warnings)
				}
				for _, decision := range selection.Tools {
					if decision.Hinted {
						t.Fatal("ignored hint reported as applied")
					}
				}
			}
		})
	}
}
