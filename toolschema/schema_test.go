package toolschema

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/genai"
)

func tools(fd *genai.FunctionDeclaration) []*genai.Tool {
	return []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{fd}}}
}
func quiet(context.Context, Warning) {}
func TestNullableEnumPreservesValues(t *testing.T) {
	nullable := true
	fd := &genai.FunctionDeclaration{Name: "example", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
		"colour": {Type: genai.TypeString, Enum: []string{"RED", "BLUE"}, Nullable: &nullable, Description: "Choose a colour"},
	}}}
	before, _ := json.Marshal(fd)
	got, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"openai", "direct"}).Prepare(t.Context(), tools(fd))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(fd)
	if string(before) != string(after) {
		t.Fatal("input mutated")
	}
	b, _ := json.Marshal(got[fd.Name].Schema)
	if strings.Contains(string(b), `"nullable"`) {
		t.Fatalf("leftover nullable: %s", b)
	}
	c := validator.NewCompiler()
	if err := c.AddResource("urn:test", got[fd.Name].Schema); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("urn:test")
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []any{nil, "RED", "BLUE"} {
		if err := schema.Validate(map[string]any{"colour": valid}); err != nil {
			t.Fatal(err)
		}
	}
	for _, invalid := range []any{"GREEN", float64(5)} {
		if schema.Validate(map[string]any{"colour": invalid}) == nil {
			t.Fatalf("accepted %v", invalid)
		}
	}
	if got[fd.Name].Strict == nil || *got[fd.Name].Strict {
		t.Fatal("optional input must not be silently required by OpenAI")
	}
}
func TestUnknownKeywordsAndMalformedSchemasAlwaysFail(t *testing.T) {
	for _, raw := range []string{
		`{"type":"object","properties":{"x":{"type":"string","nullable":true}}}`,
		`{"type":"object","required":"x"}`,
		`{"type":"object","properties":{"x":{"type":"wrong"}}}`,
		`{"type":"object","properties":{"x":{"$ref":"file:///etc/passwd"}}}`,
		`{"type":"object","properties":{"x":{"$ref":"#/$defs/missing"}}}`,
		`{"type":"object","properties":{"x":{"pattern":"["}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			for _, allow := range []bool{false, true} {
				_, err := New(Config{AllowUnsupported: allow, Warn: quiet}, Target{"openai", "direct"}).Prepare(t.Context(), tools(&genai.FunctionDeclaration{Name: "test", ParametersJsonSchema: json.RawMessage(raw)}))
				if err == nil {
					t.Fatal("accepted invalid schema")
				}
			}
		})
	}
	_, err := canonical(&genai.FunctionDeclaration{Parameters: &genai.Schema{Type: genai.TypeObject}, ParametersJsonSchema: map[string]any{"type": "object"}})
	if err == nil {
		t.Fatal("accepted both inputs")
	}
}
func TestKeywordChecksDoNotInspectData(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"nullable":{"type":"object","default":{"type":"UPPER","nullable":true},"const":{"type":"UPPER","nullable":true}}}}`)
	obj, err := canonical(&genai.FunctionDeclaration{ParametersJsonSchema: raw})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(obj)
	if !strings.Contains(string(b), `"type":"UPPER"`) {
		t.Fatalf("corrupted data: %s", b)
	}
}
func TestLossRequiresOptInAndWarningsAreDeduplicated(t *testing.T) {
	fd := &genai.FunctionDeclaration{Name: "list", ParametersJsonSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":5}}}`)}
	if _, err := New(Config{}, Target{"google", "vercel-native"}).Prepare(t.Context(), tools(fd)); err == nil {
		t.Fatal("accepted unsupported constraints")
	}
	var warnings []Warning
	p := New(Config{AllowUnsupported: true, Warn: func(_ context.Context, w Warning) { warnings = append(warnings, w) }}, Target{"google", "vercel-native"})
	for range 2 {
		got, err := p.Prepare(t.Context(), tools(fd))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(got[fd.Name].Schema)
		if strings.Contains(string(b), "minimum") || strings.Contains(string(b), "maximum") {
			t.Fatalf("constraint not removed: %s", b)
		}
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings: %+v", warnings)
	}
	for _, w := range warnings {
		if !strings.HasPrefix(w.Path, "#/properties/count/") || w.Route != "vercel-native" {
			t.Fatalf("bad diagnostic: %+v", w)
		}
	}
}
func TestGoogleNativeKeepsSupportedBounds(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer","minimum":1,"maximum":5}}}`)
	got, err := New(Config{}, Target{"google", "vertex"}).Prepare(t.Context(), tools(&genai.FunctionDeclaration{Name: "test", ParametersJsonSchema: raw}))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got["test"].Schema)
	if !strings.Contains(string(b), `"minimum":1`) {
		t.Fatalf("lost native constraint: %s", b)
	}
}
func TestTypedCardinalityAndLargeNumbers(t *testing.T) {
	n := int64(3)
	fd := &genai.FunctionDeclaration{Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"xs": {Type: genai.TypeArray, MinItems: &n, Items: &genai.Schema{Type: genai.TypeString}}}}}
	if _, err := canonical(fd); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","const":9007199254740993}}}`)
	obj, err := canonical(&genai.FunctionDeclaration{ParametersJsonSchema: raw})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(obj)
	if !strings.Contains(string(b), "9007199254740993") {
		t.Fatal("lost integer precision")
	}
}

func TestDeclaredDialectCannotChangeSilently(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"x":{"$ref":"#/$defs/n","minimum":5}},"$defs":{"n":{"type":"integer"}}}`),
		json.RawMessage(`{"type":"object","properties":{"x":{"$id":"https://example.com/embedded","$schema":"http://json-schema.org/draft-07/schema#","$ref":"#/$defs/n","minimum":5,"$defs":{"n":{"type":"integer"}}}}}`),
	} {
		for _, allow := range []bool{false, true} {
			_, err := New(Config{AllowUnsupported: allow, Warn: quiet}, Target{"openai", "direct"}).Prepare(t.Context(), tools(&genai.FunctionDeclaration{Name: "test", ParametersJsonSchema: raw}))
			if err == nil || !strings.Contains(err.Error(), "dialect") {
				t.Fatalf("expected dialect rejection, got %v", err)
			}
		}
	}
}
func TestRootAnyOfNeedsExplicitOpenAIFallback(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"],"additionalProperties":false,"anyOf":[{"required":["x"]}]}`)
	fd := &genai.FunctionDeclaration{Name: "test", ParametersJsonSchema: raw}
	if _, err := New(Config{}, Target{"openai", "direct"}).Prepare(t.Context(), tools(fd)); err == nil {
		t.Fatal("accepted root anyOf in strict mode")
	}
	got, err := New(Config{AllowUnsupported: true, Warn: quiet}, Target{"openai", "direct"}).Prepare(t.Context(), tools(fd))
	if err != nil {
		t.Fatal(err)
	}
	if got["test"].Strict == nil || *got["test"].Strict {
		t.Fatal("did not disable strict")
	}
	if got["test"].Schema["anyOf"] == nil {
		t.Fatal("silently discarded root constraint")
	}
}
