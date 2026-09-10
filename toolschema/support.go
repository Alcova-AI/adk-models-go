// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The profiles below are conservative tool-schema contracts, not model
// catalogues. Sources and trial limits are recorded in README.md. Gateway
// profiles include the publicly observed Google conversion losses.
func (p *Processor) adapt(ctx context.Context, tool string, obj map[string]any, path string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("tool %q schema %s exceeds nesting limit", tool, path)
	}
	if !slices.Contains([]string{"openai", "anthropic", "google"}, p.target.Provider) {
		return p.loss(ctx, tool, path, "provider", "no verified tool schema compatibility profile")
	}
	if p.target.Provider == "anthropic" {
		flattenNullableEnum(obj)
	}
	for _, key := range sortedKeys(obj) {
		if key == "$schema" || key == "$comment" {
			delete(obj, key)
			continue
		}
		if key == "propertyOrdering" {
			if p.target.Provider != "google" || strings.HasPrefix(p.target.Route, "vercel") {
				p.warn(ctx, Warning{tool, p.target.Provider, p.target.Route, path + "/" + key, key, "presentation hint is not portable on this route"})
				delete(obj, key)
			}
			continue
		}
		if key == "$dynamicRef" || key == "$recursiveRef" {
			return fmt.Errorf("tool %q schema %s/%s: dynamic references are not supported by this trial", tool, path, key)
		}
		// Keep reference handling explicit. Dropping $ref would remove the whole
		// referenced contract and can leave no usable tool shape.
		if key == "$ref" && p.target.Provider != "openai" {
			return fmt.Errorf("tool %q schema %s/$ref: reference conversion for %s/%s is not supported by this trial", tool, path, p.target.Provider, p.target.Route)
		}
		rootAlternative := path == "#" && (key == "anyOf" || key == "allOf") && (p.target.Provider == "anthropic" || (p.target.Provider == "google" && strings.HasPrefix(p.target.Route, "vercel")))
		if rootAlternative || !p.supports(key, obj[key]) {
			if err := p.loss(ctx, tool, path+"/"+escape(key), key, "constraint cannot be preserved by this route"); err != nil {
				return err
			}
			if key == "oneOf" && p.target.Provider == "openai" {
				// anyOf retains the possible shapes while relaxing exclusivity. Preserve
				// an existing anyOf by combining both requirements under allOf only on
				// routes that support it; otherwise removing oneOf is the safe widening.
				if _, exists := obj["anyOf"]; !exists {
					obj["anyOf"] = obj[key]
				}
			}
			delete(obj, key)
		}
	}
	return children(obj, path, func(child map[string]any, childPath string) error {
		return p.adapt(ctx, tool, child, childPath, depth+1)
	})
}

func (p *Processor) supports(key string, value any) bool {
	// Annotations are retained; they do not constitute enforced constraints.
	if slices.Contains(strings.Fields(`type properties items required enum description title default examples deprecated readOnly writeOnly $id $anchor $defs definitions $ref anyOf`), key) {
		return true
	}
	switch p.target.Provider {
	case "openai":
		return slices.Contains(strings.Fields(`additionalProperties format pattern minimum maximum exclusiveMinimum exclusiveMaximum multipleOf minLength maxLength minItems maxItems`), key)
	case "anthropic":
		if key == "additionalProperties" {
			return value == false
		}
		if key == "minItems" {
			n, ok := value.(json.Number)
			return ok && (n == "0" || n == "1")
		}
		// The API supports a limited regex grammar. We do not promise that arbitrary
		// JSON Schema patterns are accepted; callers can explicitly opt into loss.
		return key == "allOf" || key == "const" || key == "format"
	case "google":
		if strings.HasPrefix(p.target.Route, "vercel") {
			// Google conversion in the pinned public Vercel SDK loses these remaining
			// assertion keywords. Input acceptance alone is not evidence of support.
			return slices.Contains(strings.Fields(`format const minLength minItems maxItems`), key)
		}
		return slices.Contains(strings.Fields(`format additionalProperties minimum maximum minLength maxLength pattern minItems maxItems minProperties maxProperties`), key)
	}
	return false
}

func strictCompatible(schema map[string]any, provider string) bool {
	compatible := true
	if provider == "openai" {
		if _, ok := schema["anyOf"]; ok {
			compatible = false
		}
	}
	optional := 0
	var visit func(map[string]any, string) error
	visit = func(obj map[string]any, path string) error {
		_, hasProperties := obj["properties"]
		if obj["type"] == "object" || hasProperties {
			if obj["additionalProperties"] != false {
				compatible = false
			}
			required, _ := obj["required"].([]any)
			props, _ := obj["properties"].(map[string]any)
			for name := range props {
				if !slices.Contains(required, any(name)) {
					optional++
					if provider == "openai" {
						compatible = false
					}
				}
			}
		}
		return children(obj, path, visit)
	}
	_ = visit(schema, "#")
	if provider == "anthropic" && optional > 24 {
		compatible = false
	}
	return compatible
}

// A finite nullable enum has an equivalent union form. Claude can generate
// an empty string for anyOf when the requested value is null.
// Direct Claude rejects this union form in strict mode, so it needs explicit
// best-effort permission before the schema is sent.
func flattenNullableEnum(obj map[string]any) {
	branches, ok := obj["anyOf"].([]any)
	if !ok || len(branches) != 2 {
		return
	}
	for i := range branches {
		value, ok := branches[i].(map[string]any)
		null, isMap := branches[1-i].(map[string]any)
		if !ok || !isMap || len(value) != 2 || len(null) != 1 || null["type"] != "null" {
			continue
		}
		typ, isType := value["type"].(string)
		values, isEnum := value["enum"].([]any)
		if !isType || !isEnum || typ == "null" {
			continue
		}
		if _, exists := obj["type"]; exists {
			return
		}
		if _, exists := obj["enum"]; exists {
			return
		}
		obj["type"] = []any{typ, "null"}
		obj["enum"] = append(slices.Clone(values), nil)
		delete(obj, "anyOf")
		return
	}
}

func containsNullableEnum(schema map[string]any) bool {
	found := false
	var visit func(map[string]any, string) error
	visit = func(obj map[string]any, path string) error {
		types, _ := obj["type"].([]any)
		if _, exists := obj["enum"]; exists && slices.Contains(types, any("null")) {
			found = true
		}
		return children(obj, path, visit)
	}
	_ = visit(schema, "#")
	return found
}
