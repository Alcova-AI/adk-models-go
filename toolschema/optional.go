// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"context"
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
	plan := &omissionPlan{properties: map[string]*omissionPlan{}}
	required, _ := schema["required"].([]any)
	props, _ := schema["properties"].(map[string]any)
	for _, name := range sortedKeys(props) {
		child, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		childPlan, err := p.encodeOmissions(ctx, tool, child, path+"/properties/"+escape(name))
		if err != nil {
			return nil, err
		}
		if !slices.Contains(required, any(name)) && excludesNull(child) {
			if err := p.loss(ctx, tool, path+"/properties/"+escape(name), "optional", "encode omitted fields as null for Vercel Responses; restore absence in returned arguments"); err != nil {
				return nil, err
			}
			// Keep annotations on the outer schema, so the model sees the omission rule.
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
			childPlan.omitNull = true
		}
		if childPlan.omitNull || len(childPlan.properties) > 0 || childPlan.items != nil {
			plan.properties[name] = childPlan
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		child, err := p.encodeOmissions(ctx, tool, items, path+"/items")
		if err != nil {
			return nil, err
		}
		if len(child.properties) > 0 || child.items != nil {
			plan.items = child
		}
	}
	return plan, nil
}

// Only unambiguous typed schemas are widened. Alternatives already used to
// represent nullable values, references and untyped schemas keep their semantics.
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
