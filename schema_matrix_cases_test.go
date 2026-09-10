// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/genai"
)

type schemaCase struct {
	Name            string
	Schema          map[string]any
	Valid, Conflict string
	Typed           *genai.Schema
}

//go:embed testdata/schema-matrix/legacy-list-clients.genai.json
var legacyClientSchema []byte

func matrixCases(t testing.TB) []schemaCase {
	t.Helper()
	field := func(name string, rule map[string]any, good, bad string) schemaCase {
		return schemaCase{name, map[string]any{"type": "object", "properties": map[string]any{"value": rule}, "required": []any{"value"}, "additionalProperties": false}, good, bad, nil}
	}
	cases := []schemaCase{
		field("integer", map[string]any{"type": "integer"}, `{"value":3}`, `{"value":"three"}`),
		field("enum", map[string]any{"type": "string", "enum": []any{"red", "blue"}}, `{"value":"red"}`, `{"value":"green"}`),
		field("nullable-enum", map[string]any{"anyOf": []any{map[string]any{"type": "string", "enum": []any{"red"}}, map[string]any{"type": "null"}}}, `{"value":null}`, `{"value":"green"}`),
		field("minimum", map[string]any{"type": "integer", "minimum": 5}, `{"value":5}`, `{"value":4}`),
		field("maximum", map[string]any{"type": "integer", "maximum": 5}, `{"value":5}`, `{"value":6}`),
		field("exclusiveMinimum", map[string]any{"type": "number", "exclusiveMinimum": 5}, `{"value":6}`, `{"value":5}`),
		field("exclusiveMaximum", map[string]any{"type": "number", "exclusiveMaximum": 5}, `{"value":4}`, `{"value":5}`),
		field("multipleOf", map[string]any{"type": "integer", "multipleOf": 3}, `{"value":6}`, `{"value":7}`),
		field("minLength", map[string]any{"type": "string", "minLength": 3}, `{"value":"abc"}`, `{"value":"a"}`),
		field("maxLength", map[string]any{"type": "string", "maxLength": 3}, `{"value":"abc"}`, `{"value":"abcd"}`),
		field("pattern", map[string]any{"type": "string", "pattern": "^[A-Z]{3}$"}, `{"value":"ABC"}`, `{"value":"abc"}`),
		field("format-date", map[string]any{"type": "string", "format": "date"}, `{"value":"2026-09-10"}`, `{"value":"not-a-date"}`),
		field("const", map[string]any{"type": "string", "const": "fixed"}, `{"value":"fixed"}`, `{"value":"changed"}`),
		field("minItems-one", map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 1}, `{"value":[1]}`, `{"value":[]}`),
		field("minItems-two", map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 2}, `{"value":[1,2]}`, `{"value":[1]}`),
		field("maxItems", map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "maxItems": 2}, `{"value":[1,2]}`, `{"value":[1,2,3]}`),
		field("uniqueItems", map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "uniqueItems": true}, `{"value":[1,2]}`, `{"value":[1,1]}`),
		field("anyOf", map[string]any{"anyOf": []any{map[string]any{"type": "string", "enum": []any{"yes"}}, map[string]any{"type": "integer", "enum": []any{7}}}}, `{"value":7}`, `{"value":8}`),
		field("allOf", map[string]any{"type": "integer", "allOf": []any{map[string]any{"minimum": 3}, map[string]any{"maximum": 5}}}, `{"value":4}`, `{"value":6}`),
		field("oneOf", map[string]any{"type": "integer", "oneOf": []any{map[string]any{"minimum": 3}, map[string]any{"maximum": 5}}}, `{"value":7}`, `{"value":4}`),
		field("minProperties", map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "integer"}, "b": map[string]any{"type": "integer"}}, "additionalProperties": false, "minProperties": 2}, `{"value":{"a":1,"b":2}}`, `{"value":{"a":1}}`),
		field("maxProperties", map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "integer"}, "b": map[string]any{"type": "integer"}}, "additionalProperties": false, "maxProperties": 1}, `{"value":{"a":1}}`, `{"value":{"a":1,"b":2}}`),
	}
	cases = append(cases,
		schemaCase{"required", map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}, "required": []any{"value"}, "additionalProperties": false}, `{"value":1}`, `{}`, nil},
		schemaCase{"closed-object", map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}, "required": []any{"value"}, "additionalProperties": false}, `{"value":1}`, `{"value":1,"extra":2}`, nil},
		schemaCase{"pagination-not", map[string]any{"type": "object", "properties": map[string]any{"first": map[string]any{"type": "integer"}, "last": map[string]any{"type": "integer"}}, "additionalProperties": false, "not": map[string]any{"required": []any{"first", "last"}}}, `{"first":5}`, `{"first":5,"last":5}`, nil},
		schemaCase{"local-ref", map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"$ref": "#/$defs/choice"}}, "required": []any{"value"}, "additionalProperties": false, "$defs": map[string]any{"choice": map[string]any{"type": "string", "enum": []any{"red"}}}}, `{"value":"red"}`, `{"value":"blue"}`, nil},
		schemaCase{"root-anyOf", map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}, "required": []any{"value"}, "additionalProperties": false, "anyOf": []any{map[string]any{"properties": map[string]any{"value": map[string]any{"const": 1}}}, map[string]any{"properties": map[string]any{"value": map[string]any{"const": 2}}}}}, `{"value":1}`, `{"value":3}`, nil},
		schemaCase{"root-oneOf", map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}, "required": []any{"value"}, "additionalProperties": false, "oneOf": []any{map[string]any{"properties": map[string]any{"value": map[string]any{"const": 1}}}, map[string]any{"properties": map[string]any{"value": map[string]any{"const": 2}}}}}, `{"value":1}`, `{"value":3}`, nil},
	)
	for _, name := range []string{"nullable-enum", "minItems-two"} {
		for _, c := range cases {
			if c.Name == name {
				var copy map[string]any
				b, _ := json.Marshal(c.Schema)
				_ = json.Unmarshal(b, &copy)
				delete(copy, "additionalProperties")
				cases = append(cases, schemaCase{name + "-typed", copy, c.Valid, c.Conflict, nil})
				break
			}
		}
	}
	for _, suffix := range []string{"", "-typed"} {
		cases = append(cases, schemaCase{"optional-pagination" + suffix, map[string]any{"type": "object", "properties": map[string]any{"first": map[string]any{"type": "integer"}, "last": map[string]any{"type": "integer"}}}, `{"first":5}`, `{"first":"five"}`, nil})
	}

	var legacy map[string]any
	var typed genai.Schema
	if e := json.Unmarshal(legacyClientSchema, &legacy); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(legacyClientSchema, &typed); e != nil {
		t.Fatal(e)
	}
	normaliseLegacyTypes(legacy)
	cases = append(cases, schemaCase{Name: "legacy-list-clients-typed", Schema: legacy, Typed: &typed, Valid: `{"pagination":{"first":5}}`, Conflict: `{"pagination":{"first":"five"}}`})
	cases = append(cases, repeatedObjectCases()...)
	return cases
}
func matrixValidator(schema map[string]any) (*validator.Schema, error) {
	c := validator.NewCompiler()
	c.DefaultDraft(validator.Draft2020)
	c.AssertFormat()
	if err := c.AddResource("urn:matrix", schema); err != nil {
		return nil, err
	}
	return c.Compile("urn:matrix")
}
func TestSchemaMatrixOracles(t *testing.T) {
	for _, tc := range matrixCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			v, e := matrixValidator(tc.Schema)
			if e != nil {
				t.Fatal(e)
			}
			for _, p := range []struct {
				s     string
				valid bool
			}{{tc.Valid, true}, {tc.Conflict, false}} {
				var x any
				if e := json.Unmarshal([]byte(p.s), &x); e != nil {
					t.Fatal(e)
				}
				if (v.Validate(x) == nil) != p.valid {
					t.Fatalf("incorrect oracle for %s", p.s)
				}
			}
		})
	}
}

func matrixDeclaration(c schemaCase) *genai.FunctionDeclaration {
	fd := &genai.FunctionDeclaration{Name: "schema_probe", Description: "Synthetic schema test. Records arguments only; no action executes.", ParametersJsonSchema: c.Schema}
	if c.Typed != nil {
		fd.ParametersJsonSchema = nil
		fd.Parameters = c.Typed
		return fd
	}
	if !strings.HasSuffix(c.Name, "-typed") {
		return fd
	}
	fd.ParametersJsonSchema = nil
	switch c.Name {
	case "nullable-enum-typed":
		fd.Parameters = &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"value": {Type: genai.TypeString, Enum: []string{"red"}, Nullable: new(true)}}, Required: []string{"value"}}
	case "minItems-two-typed":
		fd.Parameters = &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"value": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeInteger}, MinItems: new(int64(2))}}, Required: []string{"value"}}
	case "optional-pagination-typed":
		fd.Parameters = &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"first": {Type: genai.TypeInteger}, "last": {Type: genai.TypeInteger}}}
	}
	return fd
}

// The historical typed schema uses only properties/items and uppercase types.
// Keep the full snapshot as authored; normalise its type spelling for the oracle.
func normaliseLegacyTypes(schema map[string]any) {
	if value, ok := schema["type"].(string); ok {
		schema["type"] = strings.ToLower(value)
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for _, value := range properties {
			if child, ok := value.(map[string]any); ok {
				normaliseLegacyTypes(child)
			}
		}
	}
	if child, ok := schema["items"].(map[string]any); ok {
		normaliseLegacyTypes(child)
	}
}

// Paired schemas describe the same arguments. The referenced version shares
// objects through two levels of definitions and also uses a reference in items.
func repeatedObjectCases() []schemaCase {
	object := func(properties map[string]any, required ...any) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	address := object(map[string]any{
		"street":  map[string]any{"type": "string"},
		"city":    map[string]any{"type": "string"},
		"country": map[string]any{"type": "string", "enum": []any{"AU", "NZ"}},
	}, "street", "city", "country")
	valid := `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"AU"}},"secondary":{"name":"B","address":{"street":"2 Test St","city":"Wellington","country":"NZ"}},"others":[{"name":"C","address":{"street":"3 Test St","city":"Sydney","country":"AU"}}]}`
	conflict := strings.Replace(valid, `"country":"NZ"`, `"country":"XX"`, 1)
	var cases []schemaCase
	for _, refs := range []bool{false, true} {
		suffix := "inline"
		addressUse := address
		if refs {
			suffix = "ref"
			addressUse = map[string]any{"$ref": "#/$defs/Address"}
		}
		contact := object(map[string]any{"name": map[string]any{"type": "string"}, "address": addressUse}, "name", "address")
		contactUse := contact
		if refs {
			contactUse = map[string]any{"$ref": "#/$defs/Contact"}
		}
		schema := object(map[string]any{"primary": contactUse, "secondary": contactUse, "others": map[string]any{"type": "array", "items": contactUse}}, "primary", "secondary", "others")
		nullable := object(map[string]any{"primary": contactUse, "secondary": map[string]any{"anyOf": []any{contactUse, map[string]any{"type": "null"}}}}, "primary", "secondary")
		if refs {
			schema["$defs"] = map[string]any{"Address": address, "Contact": contact}
			nullable["$defs"] = map[string]any{"Address": address, "Contact": contact}
		}
		optional := object(map[string]any{"primary": contactUse, "secondary": map[string]any{"anyOf": []any{contactUse, map[string]any{"type": "null"}}}}, "primary")
		if refs {
			optional["$defs"] = map[string]any{"Address": address, "Contact": contact}
		}
		nonNullable := object(map[string]any{"primary": contactUse, "secondary": contactUse}, "primary")
		if refs {
			nonNullable["$defs"] = map[string]any{"Address": address, "Contact": contact}
		}
		cases = append(cases,
			schemaCase{"optional-nonnullable-object-" + suffix, nonNullable, `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"AU"}}}`, `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"XX"}}}`, nil},
			schemaCase{"optional-object-" + suffix, optional, `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"AU"}}}`, `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"XX"}}}`, nil},
			schemaCase{"repeated-object-" + suffix, schema, valid, conflict, nil},
			schemaCase{"nullable-object-" + suffix, nullable, `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"AU"}},"secondary":null}`, `{"primary":{"name":"A","address":{"street":"1 Test St","city":"Melbourne","country":"XX"}},"secondary":null}`, nil},
		)
	}
	return cases
}
