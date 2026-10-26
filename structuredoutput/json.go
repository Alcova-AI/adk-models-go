// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package structuredoutput

import (
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

// ExtractJSON decodes a structured model response into T. It selects final
// response text using provider message metadata, excludes thought parts, and
// rejects missing or ambiguous final messages without falling back to drafts.
// Unphased responses retain combined-text-first, then individual-part fallbacks.
// Each decode attempt starts with a fresh T so failed attempts cannot leak fields.
// Responses must contain bare JSON; prose and Markdown fences are not stripped.
// Application schema validation and error logging remain the caller's responsibility.
func ExtractJSON[T any](content *genai.Content) (*T, error) {
	candidates, err := textCandidates(content)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, candidate := range candidates {
		var result T
		if err := json.Unmarshal([]byte(candidate), &result); err != nil {
			lastErr = err
			continue
		}
		return &result, nil
	}
	return nil, fmt.Errorf("failed to unmarshal JSON from response: %w", lastErr)
}
