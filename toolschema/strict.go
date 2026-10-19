// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package toolschema

// Anthropic limits apply to the complete request, not each tool independently.
// The caller owns task priority. Tools outside the budget remain available
// best-effort; the library does not infer relevance from schema size.
type anthropicStrictBudget struct {
	tools, optional, unions int
}

func anthropicStrictCost(schema map[string]any) (optional, unions int) {
	var visit func(map[string]any, string) error
	visit = func(obj map[string]any, path string) error {
		required, _ := obj["required"].([]any)
		props, _ := obj["properties"].(map[string]any)
		for name := range props {
			found := false
			for _, value := range required {
				if value == name {
					found = true
					break
				}
			}
			if !found {
				optional++
			}
		}
		if branches, ok := obj["anyOf"].([]any); ok && len(branches) > 1 {
			unions++
		}
		if types, ok := obj["type"].([]any); ok && len(types) > 1 {
			unions++
		}
		return children(obj, path, visit)
	}
	_ = visit(schema, "#")
	return optional, unions
}

func (b *anthropicStrictBudget) take(schema map[string]any) bool {
	optional, unions := anthropicStrictCost(schema)
	if b.tools+1 > 20 || b.optional+optional > 24 || b.unions+unions > 16 {
		return false
	}
	b.tools++
	b.optional += optional
	b.unions += unions
	return true
}

// Ordering changes only strict eligibility, not schema contents or outgoing
// tool order. Explicit hints select their named tools; nil uses declaration order.
func anthropicStrictOrder(prepared map[string]Prepared, declarations, hints []string) []string {
	order := declarations
	if hints != nil {
		order = hints
	}
	names := make([]string, 0, len(order))
	seen := map[string]bool{}
	for _, name := range order {
		tool, exists := prepared[name]
		if !exists || seen[name] || tool.Strict == nil || !*tool.Strict {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}
