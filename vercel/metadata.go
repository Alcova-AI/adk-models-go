// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkvercel

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"
	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

func responseMetadata(providers map[string]any, usage protocol.Usage) (adkmodels.Metadata, bool) {
	result := metadata.FromProviders(providers)
	result.CacheWriteInputTokens = valueOrZero(usage.InputTokens.CacheWrite)
	return result, len(providers) > 0 || result.CacheWriteInputTokens != 0
}
