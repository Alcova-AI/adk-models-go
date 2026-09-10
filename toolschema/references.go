// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"fmt"
	"strings"
)

// definition resolves the local, named definitions used by portable tool
// schemas. General URI scopes, anchors and recursive schemas are deliberately
// not part of the cross-provider contract.
func definition(root map[string]any, ref string) (map[string]any, bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 || parts[0] != "#" || (parts[1] != "$defs" && parts[1] != "definitions") {
		return nil, false
	}
	name := strings.ReplaceAll(strings.ReplaceAll(parts[2], "~1", "/"), "~0", "~")
	defs, _ := root[parts[1]].(map[string]any)
	value, ok := defs[name].(map[string]any)
	return value, ok
}

func checkReferences(root map[string]any, provider string) error {
	if !hasReference(root) {
		return nil
	}
	active := map[string]bool{}
	checked := map[string]bool{}
	var visit func(map[string]any, string) error
	visit = func(obj map[string]any, path string) error {
		if _, scoped := obj["$id"]; scoped && path != "#" {
			return fmt.Errorf("schema %s/$id: nested reference scopes are not supported", path)
		}
		if ref, ok := obj["$ref"].(string); ok {
			target, found := definition(root, ref)
			if !found {
				return fmt.Errorf("schema %s/$ref: only named local definitions are supported on %s", path, provider)
			}
			if active[ref] {
				return fmt.Errorf("schema %s/$ref: recursive references are not supported on %s", path, provider)
			}
			if !checked[ref] {
				active[ref] = true
				if err := visit(target, ref); err != nil {
					return err
				}
				delete(active, ref)
				checked[ref] = true
			}
		}
		if provider == "anthropic" {
			if branches, ok := obj["allOf"].([]any); ok {
				for _, branch := range branches {
					if child, ok := branch.(map[string]any); ok && hasReference(child) {
						return fmt.Errorf("schema %s/allOf: references inside allOf are not supported on anthropic", path)
					}
				}
			}
		}
		return children(obj, path, visit)
	}
	return visit(root, "#")
}

func hasReference(obj map[string]any) bool {
	if _, ok := obj["$ref"]; ok {
		return true
	}
	found := false
	_ = children(obj, "#", func(child map[string]any, _ string) error { found = found || hasReference(child); return nil })
	return found
}
