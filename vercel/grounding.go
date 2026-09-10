// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

// groundingMetadata retains the provider's chunk order because support indices
// refer to that order. Wire sources are a fallback when grounding metadata is absent.
func groundingMetadata(metadata map[string]any, sources []*genai.GroundingChunk) (*genai.GroundingMetadata, error) {
	for _, namespace := range []string{"googleVertex", "vertex", "google"} {
		provider, ok := metadata[namespace].(map[string]any)
		if !ok || provider["groundingMetadata"] == nil {
			continue
		}
		raw, err := json.Marshal(provider["groundingMetadata"])
		if err != nil {
			return nil, fmt.Errorf("vercel: encode grounding metadata: %w", err)
		}
		var grounding genai.GroundingMetadata
		if err := json.Unmarshal(raw, &grounding); err != nil {
			return nil, fmt.Errorf("vercel: decode grounding metadata: %w", err)
		}
		return &grounding, nil
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return &genai.GroundingMetadata{GroundingChunks: sources}, nil
}

func webSource(sourceType, url, title string) *genai.GroundingChunk {
	if sourceType != "url" || url == "" {
		return nil
	}
	return &genai.GroundingChunk{Web: &genai.GroundingChunkWeb{URI: url, Title: title}}
}
