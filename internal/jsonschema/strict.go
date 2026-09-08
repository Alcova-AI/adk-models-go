// Copyright 2025 Google LLC
// Modified by Alcova AI, 2026.
// Licensed under the Apache License, Version 2.0.

package jsonschema

import "sort"

// EnforceOpenAI normalises an owned schema for OpenAI strict structured output.
func EnforceOpenAI(value any) {
	schema, ok := value.(map[string]any)
	if !ok {
		return
	}
	if _, hasRef := schema["$ref"]; hasRef {
		for key := range schema {
			if key != "$ref" {
				delete(schema, key)
			}
		}
		return
	}
	typeValue, hasType := schema["type"]
	properties, hasProperties := schema["properties"]
	if hasType && typeValue == "object" && hasProperties {
		schema["additionalProperties"] = false
		if propertyMap, ok := properties.(map[string]any); ok {
			required := make([]string, 0, len(propertyMap))
			for key := range propertyMap {
				required = append(required, key)
			}
			sort.Strings(required)
			schema["required"] = required
		}
	}
	if definitions, ok := schema["$defs"].(map[string]any); ok {
		for _, definition := range definitions {
			EnforceOpenAI(definition)
		}
	}
	if propertyMap, ok := properties.(map[string]any); ok {
		for _, property := range propertyMap {
			EnforceOpenAI(property)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if values, ok := schema[key].([]any); ok {
			for _, child := range values {
				EnforceOpenAI(child)
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		EnforceOpenAI(items)
	}
}
