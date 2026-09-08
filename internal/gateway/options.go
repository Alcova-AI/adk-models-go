// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package gateway

import (
	"encoding/json"
	"fmt"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/family"
	"google.golang.org/genai"
)

// Options encodes one shared family mapping for all gateway transports.
func Options(cfg adkmodels.VercelConfig, f family.Family, level genai.ThinkingLevel, include bool, openAI adkmodels.OpenAIReasoningConfig) (map[string]map[string]any, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	mapped, err := family.Map(f, level)
	if err != nil {
		return nil, err
	}
	result := Clone(cfg.ProviderOptions)
	values := familyOptions(f, mapped, include, openAI)
	existing := result[string(f)]
	if existing == nil {
		existing = make(map[string]any)
	}
	for k, v := range values {
		if _, ok := existing[k]; ok {
			return nil, fmt.Errorf("provider option %s.%s conflicts with a typed setting", f, k)
		}
		existing[k] = v
	}
	if len(existing) > 0 {
		result[string(f)] = existing
	}
	encoded, err := gatewayOptions(cfg)
	if err != nil {
		return nil, err
	}
	result["gateway"] = encoded
	return result, nil
}

func familyOptions(f family.Family, r family.Reasoning, include bool, openAI adkmodels.OpenAIReasoningConfig) map[string]any {
	values := make(map[string]any)
	switch f {
	case family.OpenAI:
		values["store"] = false
		if openAI.Context != "" {
			values["reasoningContext"] = string(openAI.Context)
		}
		if openAI.Mode != "" {
			values["reasoningMode"] = string(openAI.Mode)
		}
		if r.Effort != "" {
			values["reasoningEffort"] = r.Effort
		}
		if include {
			summary := string(openAI.Summary)
			if summary == "" {
				summary = "auto"
			}
			values["reasoningSummary"] = summary
		}
	case family.Anthropic:
		if r.Thinking != "" {
			values["thinking"] = map[string]any{"type": r.Thinking}
		}
		if r.Effort != "" {
			values["effort"] = r.Effort
		}
	case family.Gemini:
		thinking := make(map[string]any)
		if r.Effort != "" {
			thinking["thinkingLevel"] = r.Effort
		}
		if include {
			thinking["includeThoughts"] = true
		}
		if len(thinking) > 0 {
			values["thinkingConfig"] = thinking
		}
	case family.ZAI:
		if r.Thinking != "" {
			values["thinking"] = map[string]any{"type": r.Thinking}
		}
		if r.Effort != "" {
			values["reasoningEffort"] = r.Effort
		}
	}
	return values
}

func gatewayOptions(c adkmodels.VercelConfig) (map[string]any, error) {
	values := copyValues(c.GatewayOptions)
	// Optional values keep their original gateway JSON names and omission rules.
	optional := struct {
		BYOK                   map[string][]map[string]any        `json:"byok,omitempty"`
		Caching                adkmodels.GatewayCaching           `json:"caching,omitempty"`
		DisallowPromptTraining bool                               `json:"disallowPromptTraining,omitempty"`
		Has                    []adkmodels.GatewayCapability      `json:"has,omitempty"`
		IdempotencyKey         string                             `json:"idempotencyKey,omitempty"`
		Models                 []string                           `json:"models,omitempty"`
		Only                   []string                           `json:"only,omitempty"`
		Order                  []string                           `json:"order,omitempty"`
		ProviderTimeouts       *adkmodels.GatewayProviderTimeouts `json:"providerTimeouts,omitempty"`
		QuotaEntityID          string                             `json:"quotaEntityId,omitempty"`
		ServiceTier            adkmodels.GatewayServiceTier       `json:"serviceTier,omitempty"`
		Sort                   adkmodels.GatewaySort              `json:"sort,omitempty"`
		Tags                   []string                           `json:"tags,omitempty"`
		User                   string                             `json:"user,omitempty"`
		ZeroDataRetention      bool                               `json:"zeroDataRetention"`
	}{c.BYOK, c.Caching, c.DisallowPromptTraining, c.Has, c.IdempotencyKey, c.Models, c.Only, c.Order, c.ProviderTimeouts, c.QuotaEntityID, c.ServiceTier, c.Sort, c.Tags, c.User, c.ZeroDataRetention}
	raw, err := json.Marshal(optional)
	if err != nil {
		return nil, fmt.Errorf("encode gateway options: %w", err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("decode gateway options: %w", err)
	}
	for k, v := range encoded {
		values[k] = v
	}
	return values, nil
}

func Clone(source map[string]map[string]any) map[string]map[string]any {
	result := make(map[string]map[string]any, len(source)+1)
	for namespace, values := range source {
		result[namespace] = copyValues(values)
	}
	return result
}
func copyValues(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+1)
	for k, v := range source {
		result[k] = v
	}
	return result
}

// ForcedTools preserves the existing Anthropic forced-tool exception. Both
// representations must drop adaptive controls so provider options cannot
// restore thinking after the Messages fields have suppressed it.
func ForcedTools(options map[string]map[string]any, cfg *genai.GenerateContentConfig) {
	if cfg == nil || cfg.ToolConfig == nil || cfg.ToolConfig.FunctionCallingConfig == nil || cfg.ToolConfig.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny {
		return
	}
	values := options["anthropic"]
	thinking, ok := values["thinking"].(map[string]any)
	if !ok {
		return
	}
	if thinking["type"] == "adaptive" {
		delete(values, "effort")
	}
	delete(values, "thinking")
}
