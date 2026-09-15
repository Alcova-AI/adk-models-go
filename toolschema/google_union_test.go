package toolschema

import (
	"encoding/json"
	"reflect"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/genai"
)

func TestGoogleGatewayUnionsPreserveAcceptedValues(t *testing.T) {
	for _, field := range []string{
		`{"type":["null","array"],"description":"Files to copy","items":{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"]}}`,
		`{"type":["null","object"],"description":"Optional resource","properties":{"path":{"type":"string"}},"required":["path"]}`,
		`{"type":["null","string"],"enum":["red"],"description":"Null remains disallowed by enum"}`,
		`{"anyOf":[{"type":"null"},{"type":"string","enum":["red"],"description":"Branch guidance"}],"description":"Parent guidance"}`,
		`{"anyOf":[{"anyOf":[{"type":"null"},{"type":"string","enum":["red"]}]}],"description":"Nested guidance"}`,
	} {
		t.Run(field, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"object","properties":{"value":` + field + `},"required":["value"]}`)
			fd := &genai.FunctionDeclaration{Name: "example", ParametersJsonSchema: raw}
			original, err := canonical(fd)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(fd)
			for _, route := range []string{"vercel-anthropic", "vercel-openai", "vercel-native"} {
				prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"google", route}).Prepare(t.Context(), tools(fd))
				if err != nil {
					t.Fatal(err)
				}
				actual := prepared[fd.Name].Schema
				assertGoogleUnionShape(t, actual)
				wantSchema := compileGoogleTestSchema(t, original)
				gotSchema := compileGoogleTestSchema(t, actual)
				for _, sample := range []any{nil, "red", "blue", float64(7), []any{}, []any{map[string]any{"destination": "x"}}, []any{map[string]any{}}, map[string]any{"path": "x"}, map[string]any{}} {
					value := map[string]any{"value": sample}
					if (wantSchema.Validate(value) == nil) != (gotSchema.Validate(value) == nil) {
						t.Fatalf("%s changed validity of %#v", route, sample)
					}
				}
				if (wantSchema.Validate(map[string]any{}) == nil) != (gotSchema.Validate(map[string]any{}) == nil) {
					t.Fatal("changed omission")
				}
			}
			after, _ := json.Marshal(fd)
			if string(before) != string(after) {
				t.Fatal("mutated caller schema")
			}
			// Native Google and other providers retain the original union representation.
			for _, target := range []Target{{"google", "vertex"}, {"openai", "direct"}} {
				prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, target).Prepare(t.Context(), tools(fd))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(prepared[fd.Name].Schema, original) {
					t.Fatalf("changed %v", target)
				}
			}
		})
	}
}

func assertGoogleUnionShape(t *testing.T, obj map[string]any) {
	t.Helper()
	if _, ok := obj["type"].([]any); ok {
		t.Fatal("type list remains")
	}
	if _, ok := obj["anyOf"]; ok && len(obj) != 1 {
		t.Fatalf("anyOf siblings: %#v", obj)
	}
	if err := children(obj, "#", func(child map[string]any, _ string) error { assertGoogleUnionShape(t, child); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleGatewayUnionReferencesAndTypedNullable(t *testing.T) {
	for _, fd := range []*genai.FunctionDeclaration{
		{Name: "ref", ParametersJsonSchema: json.RawMessage(`{"type":"object","properties":{"files":{"$ref":"#/$defs/files"}},"$defs":{"files":{"type":["array","null"],"description":"Files","items":{"$ref":"#/$defs/file"}},"file":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}`)},
		{Name: "typed", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"files": {Type: genai.TypeArray, Nullable: new(true), Description: "Files", Items: &genai.Schema{Type: genai.TypeString}}}}},
	} {
		prepared, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"google", "vercel-native"}).Prepare(t.Context(), tools(fd))
		if err != nil {
			t.Fatal(err)
		}
		assertGoogleUnionShape(t, prepared[fd.Name].Schema)
		_ = compileGoogleTestSchema(t, prepared[fd.Name].Schema)
	}
}

func TestGoogleGatewayRejectsUnionAssertionSiblings(t *testing.T) {
	for _, field := range []string{`{"anyOf":[{"type":"integer"},{"type":"number"}],"enum":[2]}`, `{"type":["string","null"],"anyOf":[{"enum":["red"]}]}`} {
		fd := &genai.FunctionDeclaration{Name: "example", ParametersJsonSchema: json.RawMessage(`{"type":"object","properties":{"value":` + field + `}}`)}
		// An assertion which this route preserves must not be silently discarded.
		for _, allow := range []bool{false, true} {
			if _, err := New(Config{AllowUnsupported: allow, Warn: quiet}, Target{"google", "vercel-native"}).Prepare(t.Context(), tools(fd)); err == nil {
				t.Fatal("accepted unsupported combination")
			}
		}
	}
}

func compileGoogleTestSchema(t *testing.T, schema map[string]any) *validator.Schema {
	t.Helper()
	c := validator.NewCompiler()
	if err := c.AddResource("urn:google-test", schema); err != nil {
		t.Fatal(err)
	}
	v, err := c.Compile("urn:google-test")
	if err != nil {
		t.Fatal(err)
	}
	return v
}
