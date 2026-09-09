// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package toolschema validates tool schemas and prepares them for a provider.
// Compatibility checking and provider constrained decoding are separate: an
// explicit AllowUnsupported opt-in permits documented losses, never malformed
// schemas. Callers must still validate arguments before executing a tool.
package toolschema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/genai"
)

// Target identifies the receiving format and provider, not just the model name.
// Provider is openai, anthropic or google. Route is direct, vertex,
// vercel-native, vercel-openai or vercel-anthropic.
type Target struct{ Provider, Route string }

// Config controls compatibility losses. The zero value rejects them. Warn is
// optional; the default logs diagnostics without schema values or arguments.
type Config struct {
	AllowUnsupported bool
	Warn             func(context.Context, Warning)
}

type Warning struct{ Tool, Provider, Route, Path, Keyword, Reason string }

func (w Warning) Error() string {
	return fmt.Sprintf("tool %q schema %s (%s): %s on %s/%s", w.Tool, w.Path, w.Keyword, w.Reason, w.Provider, w.Route)
}

type Prepared struct {
	Schema map[string]any
	// Strict is nil for native Gemini, where the SDK owns the wire format.
	Strict *bool
}

// Processor is safe for concurrent requests. Its bounded diagnostic cache
// deduplicates warnings for one model instance without retaining schema values.
type Processor struct {
	config Config
	target Target
	mu     sync.Mutex
	seen   map[Warning]bool
	order  []Warning
}

func New(config Config, target Target) *Processor {
	return &Processor{config: config, target: target, seen: make(map[Warning]bool)}
}

func (p *Processor) Prepare(ctx context.Context, tools []*genai.Tool) (map[string]Prepared, error) {
	result := make(map[string]Prepared)
	for i, t := range tools {
		if t == nil {
			return nil, fmt.Errorf("tool %d is nil", i)
		}
		for j, fd := range t.FunctionDeclarations {
			if fd == nil || fd.Name == "" {
				return nil, fmt.Errorf("tool %d function %d has no name", i, j)
			}
			if _, exists := result[fd.Name]; exists {
				return nil, fmt.Errorf("duplicate tool %q", fd.Name)
			}
			schema, err := canonical(fd)
			if err != nil {
				return nil, fmt.Errorf("tool %q: %w", fd.Name, err)
			}
			if err = p.adapt(ctx, fd.Name, schema, "#", 0); err != nil {
				return nil, err
			}
			if err = validate(schema); err != nil {
				return nil, fmt.Errorf("tool %q converted schema: %w", fd.Name, err)
			}
			prepared := Prepared{Schema: schema}
			if p.target.Provider == "openai" || p.target.Provider == "anthropic" {
				strict := strictCompatible(schema, p.target.Provider)
				if !strict {
					if err := p.loss(ctx, fd.Name, "#", "strict", "schema requires best-effort calling because strict-mode shape requirements cannot be preserved"); err != nil {
						return nil, err
					}
				}
				prepared.Strict = &strict
			}
			result[fd.Name] = prepared
		}
	}
	return result, nil
}

func canonical(fd *genai.FunctionDeclaration) (map[string]any, error) {
	if fd.Parameters != nil && fd.ParametersJsonSchema != nil {
		return nil, fmt.Errorf("supply Parameters or ParametersJsonSchema, not both")
	}
	var source any = fd.ParametersJsonSchema
	typed := fd.Parameters != nil
	if typed {
		source = fd.Parameters
	}
	if source == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, nil
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}
	// RawMessage is the supported raw-byte form. Plain []byte is deliberately
	// not special-cased: JSON encoding treats it as base64, not as a schema.
	value, err := validator.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema must be a JSON object: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("schema must be a JSON object")
	}
	if err = normalise(obj, typed, "#", 0); err != nil {
		return nil, err
	}
	if obj["type"] != "object" {
		return nil, fmt.Errorf("tool schema root must have type object")
	}
	if err = validate(obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func normalise(obj map[string]any, typed bool, path string, depth int) error {
	if dialect, declared := obj["$schema"]; declared && dialect != "https://json-schema.org/draft/2020-12/schema" {
		return fmt.Errorf("schema %s: only the JSON Schema 2020-12 dialect is supported by this trial", path)
	}
	if depth > 64 {
		return fmt.Errorf("schema %s exceeds nesting limit", path)
	}
	if typed {
		if typ, ok := obj["type"].(string); ok {
			obj["type"] = strings.ToLower(typ)
		}
		for _, key := range []string{"minItems", "maxItems", "minLength", "maxLength", "minProperties", "maxProperties"} {
			if value, ok := obj[key].(string); ok {
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("schema %s/%s must be an integer", path, key)
				}
				obj[key] = json.Number(strconv.FormatInt(n, 10))
			}
		}
		if example, ok := obj["example"]; ok {
			obj["examples"] = []any{example}
			delete(obj, "example")
		}
	}
	for _, key := range sortedKeys(obj) {
		if typed && (key == "nullable" || key == "propertyOrdering") {
			continue
		}
		if !knownKeyword(key) {
			return fmt.Errorf("schema %s/%s: unknown JSON Schema keyword", path, escape(key))
		}
		if key == "$ref" || key == "$dynamicRef" || key == "$recursiveRef" {
			ref, ok := obj[key].(string)
			if !ok || !strings.HasPrefix(ref, "#") {
				return fmt.Errorf("schema %s/%s: only local references are supported", path, key)
			}
		}
	}
	if err := children(obj, path, func(child map[string]any, childPath string) error { return normalise(child, typed, childPath, depth+1) }); err != nil {
		return err
	}
	if typed {
		nullable, _ := obj["nullable"].(bool)
		delete(obj, "nullable")
		if nullable {
			// nullable applies to the complete schema, including enum and anyOf.
			// Merely appending null to type would still leave enum rejecting null.
			value := make(map[string]any, len(obj))
			for k, v := range obj {
				value[k] = v
				delete(obj, k)
			}
			for _, k := range []string{"description", "title", "default", "examples", "propertyOrdering"} {
				if v, ok := value[k]; ok {
					obj[k] = v
					delete(value, k)
				}
			}
			obj["anyOf"] = []any{value, map[string]any{"type": "null"}}
		}
	}
	return nil
}

func knownKeyword(key string) bool {
	return slices.Contains(strings.Fields(`$schema $id $anchor $ref $defs definitions $comment $vocabulary $dynamicAnchor $dynamicRef $recursiveAnchor $recursiveRef type enum const title description default examples deprecated readOnly writeOnly format contentEncoding contentMediaType contentSchema multipleOf minimum maximum exclusiveMinimum exclusiveMaximum minLength maxLength pattern items prefixItems additionalItems contains minContains maxContains minItems maxItems uniqueItems properties patternProperties additionalProperties unevaluatedProperties propertyNames required dependentRequired dependentSchemas dependencies minProperties maxProperties unevaluatedItems allOf anyOf oneOf not if then else`), key)
}

func children(obj map[string]any, path string, visit func(map[string]any, string) error) error {
	for _, key := range sortedKeys(obj) {
		value := obj[key]
		base := path + "/" + escape(key)
		switch key {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
			if entries, ok := value.(map[string]any); ok {
				for _, name := range sortedKeys(entries) {
					if child, ok := entries[name].(map[string]any); ok {
						if err := visit(child, base+"/"+escape(name)); err != nil {
							return err
						}
					}
				}
			}
		case "items", "additionalItems", "additionalProperties", "unevaluatedProperties", "unevaluatedItems", "propertyNames", "contains", "not", "if", "then", "else", "contentSchema":
			if child, ok := value.(map[string]any); ok {
				if err := visit(child, base); err != nil {
					return err
				}
			}
			if key == "items" {
				if entries, ok := value.([]any); ok {
					for i, item := range entries {
						if child, ok := item.(map[string]any); ok {
							if err := visit(child, fmt.Sprintf("%s/%d", base, i)); err != nil {
								return err
							}
						}
					}
				}
			}
		case "allOf", "anyOf", "oneOf", "prefixItems":
			if entries, ok := value.([]any); ok {
				for i, item := range entries {
					if child, ok := item.(map[string]any); ok {
						if err := visit(child, fmt.Sprintf("%s/%d", base, i)); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func validate(schema map[string]any) error {
	compiler := validator.NewCompiler()
	compiler.DefaultDraft(validator.Draft2020)
	compiler.UseLoader(noExternalLoader{})
	const id = "urn:adk:tool-schema"
	if err := compiler.AddResource(id, schema); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	if _, err := compiler.Compile(id); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	return nil
}

type noExternalLoader struct{}

func (noExternalLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("external schema loading is disabled")
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
func escape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func (p *Processor) loss(ctx context.Context, tool, path, keyword, reason string) error {
	w := Warning{tool, p.target.Provider, p.target.Route, path, keyword, reason}
	if !p.config.AllowUnsupported {
		return w
	}
	p.warn(ctx, w)
	return nil
}
func (p *Processor) warn(ctx context.Context, w Warning) {
	p.mu.Lock()
	if p.seen[w] {
		p.mu.Unlock()
		return
	}
	if len(p.order) == 256 {
		delete(p.seen, p.order[0])
		p.order = p.order[1:]
	}
	p.seen[w] = true
	p.order = append(p.order, w)
	p.mu.Unlock()
	if p.config.Warn != nil {
		p.config.Warn(ctx, w)
		return
	}
	slog.LogAttrs(ctx, slog.LevelWarn, "Tool schema uses weaker provider enforcement", slog.String("tool", w.Tool), slog.String("provider", w.Provider), slog.String("route", w.Route), slog.String("schema_path", w.Path), slog.String("keyword", w.Keyword), slog.String("reason", w.Reason))
}
