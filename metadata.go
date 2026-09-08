// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels

import "google.golang.org/adk/v2/model"

const MetadataKey = "adkmodels.response"

// Metadata contains shared response facts and unfiltered provider metadata.
// UsageMetadata on the ADK response remains the standard token-usage interface.
// Callers decide which metadata to log or trace.
type Metadata struct {
	ResponseID                 string         `json:"response_id,omitempty"`
	CacheWriteInputTokens      int64          `json:"cache_write_input_tokens,omitempty"`
	CacheCreation5mInputTokens int64          `json:"cache_creation_5m_input_tokens,omitempty"`
	CacheCreation1hInputTokens int64          `json:"cache_creation_1h_input_tokens,omitempty"`
	GenerationID               string         `json:"generation_id,omitempty"`
	ResolvedProvider           string         `json:"resolved_provider,omitempty"`
	OriginalModelID            string         `json:"original_model_id,omitempty"`
	CanonicalModel             string         `json:"canonical_model,omitempty"`
	ModelAttemptCount          int            `json:"model_attempt_count,omitempty"`
	ProviderAttemptCount       int            `json:"provider_attempt_count,omitempty"`
	CostUSD                    *float64       `json:"cost_usd,omitempty"`
	ProviderMetadata           map[string]any `json:"provider_metadata,omitempty"`
}

func MetadataFromResponse(response *model.LLMResponse) (Metadata, bool) {
	if response == nil || response.CustomMetadata == nil {
		return Metadata{}, false
	}
	metadata, ok := response.CustomMetadata[MetadataKey].(Metadata)
	return metadata, ok
}
