// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"

	"google.golang.org/adk/v2/model"
)

type omissionPlan struct {
	omitNull   bool
	properties map[string]*omissionPlan
	items      *omissionPlan
}

// Vercel Responses currently fills optional OpenAI fields despite strict:false.
// Encode absence as null on that route, then restore absence in final arguments.
// Existing nullable values retain their meaning; this is never a blanket null scrub.
func (p *Processor) encodeOmissions(ctx context.Context, tool string, schema map[string]any, path string) (*omissionPlan, error) {
	encoder := omissionEncoder{processor: p, ctx: ctx, tool: tool, root: schema, references: map[string]*omissionPlan{}}
	return encoder.plan(schema, path)
}

type omissionEncoder struct {
	processor  *Processor
	ctx        context.Context
	tool       string
	root       map[string]any
	references map[string]*omissionPlan
}

func (e *omissionEncoder) plan(schema map[string]any, path string) (*omissionPlan, error) {
	if ref, ok := schema["$ref"].(string); ok && referenceWithAnnotations(schema) {
		if target, found := definition(e.root, ref); found {
			if plan, seen := e.references[ref]; seen {
				return plan, nil
			}
			plan, err := e.plan(target, ref)
			if err != nil {
				return nil, err
			}
			e.references[ref] = plan
			return plan, nil
		}
	}
	// A value-or-null union is unambiguous. Follow its value branch for nested
	// omissions, but never turn its legitimate outer null into absence.
	if value, index := nullableValue(schema); value != nil {
		return e.plan(value, fmt.Sprintf("%s/anyOf/%d", path, index))
	}
	plan := &omissionPlan{properties: map[string]*omissionPlan{}}
	if err := e.fill(plan, schema, path); err != nil {
		return nil, err
	}
	return plan, nil
}

func (e *omissionEncoder) fill(plan *omissionPlan, schema map[string]any, path string) error {
	required, _ := schema["required"].([]any)
	props, _ := schema["properties"].(map[string]any)
	for _, name := range sortedKeys(props) {
		child, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		childPlan, err := e.plan(child, path+"/properties/"+escape(name))
		if err != nil {
			return err
		}
		if !slices.Contains(required, any(name)) && referencedExcludesNull(e.root, child, map[string]bool{}) {
			if err := e.processor.loss(e.ctx, e.tool, path+"/properties/"+escape(name), "optional", "encode omitted fields as null for Vercel Responses; restore absence in returned arguments"); err != nil {
				return err
			}
			value := map[string]any{}
			for key, v := range child {
				value[key] = v
				delete(child, key)
			}
			for _, key := range []string{"description", "title", "default", "examples"} {
				if v, ok := value[key]; ok {
					child[key] = v
					delete(value, key)
				}
			}
			description, _ := child["description"].(string)
			child["description"] = strings.TrimSpace(description + " Use null when this input was not provided; do not invent a value.")
			child["anyOf"] = []any{value, map[string]any{"type": "null"}}
			// A definition can be used by both required and optional properties.
			// Only this occurrence owns the omission marker, never the shared plan.
			copyPlan := *childPlan
			copyPlan.omitNull = true
			childPlan = &copyPlan
		}
		plan.properties[name] = childPlan
	}
	if items, ok := schema["items"].(map[string]any); ok {
		child, err := e.plan(items, path+"/items")
		if err != nil {
			return err
		}
		plan.items = child
	}
	return nil
}

func referenceWithAnnotations(schema map[string]any) bool {
	for key := range schema {
		if !slices.Contains([]string{"$ref", "description", "title", "default", "examples", "deprecated", "readOnly", "writeOnly"}, key) {
			return false
		}
	}
	return true
}

func referencedExcludesNull(root, schema map[string]any, seen map[string]bool) bool {
	if ref, ok := schema["$ref"].(string); ok && referenceWithAnnotations(schema) {
		if seen[ref] {
			return false
		}
		seen[ref] = true
		if target, found := definition(root, ref); found {
			return referencedExcludesNull(root, target, seen)
		}
	}
	return excludesNull(schema)
}

func nullableValue(schema map[string]any) (map[string]any, int) {
	for key := range schema {
		if !slices.Contains([]string{"anyOf", "description", "title", "default", "examples", "deprecated", "readOnly", "writeOnly"}, key) {
			return nil, 0
		}
	}
	branches, ok := schema["anyOf"].([]any)
	if !ok || len(branches) != 2 {
		return nil, 0
	}
	for i, branch := range branches {
		null, ok := branch.(map[string]any)
		if ok && len(null) == 1 && null["type"] == "null" {
			value, _ := branches[1-i].(map[string]any)
			return value, 1 - i
		}
	}
	return nil, 0
}

// Only unambiguous non-nullable types are widened. Arbitrary alternatives
// and untyped schemas keep their semantics.
func excludesNull(schema map[string]any) bool {
	switch typ := schema["type"].(type) {
	case string:
		return typ != "null" && typ != ""
	case []any:
		return !slices.Contains(typ, any("null"))
	}
	return false
}

func (p *omissionPlan) restore(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, v := range value {
			child := p.properties[key]
			if child != nil {
				if v == nil && child.omitNull {
					continue
				}
				v = child.restore(v)
			}
			result[key] = v
		}
		return result
	case []any:
		if p.items == nil {
			return value
		}
		result := make([]any, len(value))
		for i, v := range value {
			result[i] = p.items.restore(v)
		}
		return result
	}
	return value
}

// RestoreOmissions reverses only adapter-added null placeholders on final tool
// calls. Partial deltas stay provider-native; callers execute final calls only.
func RestoreOmissions(sequence iter.Seq2[*model.LLMResponse, error], prepared map[string]Prepared) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for response, err := range sequence {
			if response != nil && !response.Partial && response.Content != nil {
				copyResponse := *response
				copyContent := *response.Content
				copyResponse.Content = &copyContent
				copyContent.Parts = slices.Clone(response.Content.Parts)
				for i, part := range copyContent.Parts {
					if part == nil || part.FunctionCall == nil {
						continue
					}
					plan := prepared[part.FunctionCall.Name].omissions
					if plan == nil {
						continue
					}
					copyPart := *part
					copyCall := *part.FunctionCall
					copyPart.FunctionCall = &copyCall
					copyContent.Parts[i] = &copyPart
					args := plan.restore(copyCall.Args)
					copyCall.Args, _ = args.(map[string]any)
				}
				response = &copyResponse
			}
			if !yield(response, err) {
				return
			}
		}
	}
}
