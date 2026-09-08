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
	result.Only = slices.Clone(source.Only)
	result.Order = slices.Clone(source.Order)
	result.Models = slices.Clone(source.Models)
	result.Has = slices.Clone(source.Has)
	result.Tags = slices.Clone(source.Tags)
	result.ProviderOptions = Clone(source.ProviderOptions)
	result.GatewayOptions = maps.Clone(source.GatewayOptions)
	if source.ProviderTimeouts != nil {
		result.ProviderTimeouts = &adkmodels.GatewayProviderTimeouts{BYOK: maps.Clone(source.ProviderTimeouts.BYOK)}
	}
	return &result
}
