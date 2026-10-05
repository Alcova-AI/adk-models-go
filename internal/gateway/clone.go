// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package gateway

import (
	"maps"
	"slices"

	adkmodels "github.com/Alcova-AI/adk-models-go"
)

// CloneConfig preserves the SDK adapters' configuration snapshot: routing
// slices and option maps can be reused by the caller after construction.
func CloneConfig(source *adkmodels.VercelConfig) *adkmodels.VercelConfig {
	if source == nil {
		return nil
	}
	result := *source
	result.BYOK = cloneBYOK(source.BYOK)
	result.Only = slices.Clone(source.Only)
	result.Order = slices.Clone(source.Order)
	result.Models = slices.Clone(source.Models)
	result.Has = slices.Clone(source.Has)
	result.Tags = slices.Clone(source.Tags)
	result.ProviderOptions = Clone(source.ProviderOptions)
	result.SystemInstructionCacheOptions = cloneCacheOptions(source.SystemInstructionCacheOptions)
	result.ConversationHistoryCacheOptions = cloneCacheOptions(source.ConversationHistoryCacheOptions)
	result.GatewayOptions = maps.Clone(source.GatewayOptions)
	if source.ProviderTimeouts != nil {
		result.ProviderTimeouts = &adkmodels.GatewayProviderTimeouts{BYOK: maps.Clone(source.ProviderTimeouts.BYOK)}
	}
	return &result
}

func cloneBYOK(source map[string][]map[string]any) map[string][]map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string][]map[string]any, len(source))
	for provider, credentials := range source {
		entries := slices.Clone(credentials)
		for i, credential := range entries {
			entries[i] = maps.Clone(credential)
		}
		result[provider] = entries
	}
	return result
}

func cloneCacheOptions(source map[string]map[string]any) map[string]map[string]any {
	result := Clone(source)
	for _, values := range result {
		for key, value := range values {
			values[key] = cloneCacheValue(value)
		}
	}
	return result
}
func cloneCacheValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(v))
		for k, child := range v {
			copy[k] = cloneCacheValue(child)
		}
		return copy
	case []any:
		copy := make([]any, len(v))
		for i, child := range v {
			copy[i] = cloneCacheValue(child)
		}
		return copy
	default:
		return value
	}
}
