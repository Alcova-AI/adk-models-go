// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package structuredoutput extracts typed JSON from model responses.
// Provider-specific message phases remain an adapter implementation detail.
package structuredoutput

import (
	"encoding/json"
	"fmt"
	"strings"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
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

// textCandidates returns whole-text candidates for JSON decoding, in order.
// For explicitly phased output it returns only the selected final message,
// joining fragments of that message. Commentary stays in the original content.
// Missing or ambiguous final messages fail rather than falling back to drafts.
//
// Unphased responses retain the legacy combined-text-first fallback to individual
// text parts. Thought parts are excluded in both cases. ExtractJSON owns JSON
// decoding; callers own application schema validation. Content is not modified.
func textCandidates(content *genai.Content) ([]string, error) {
	if content == nil {
		return nil, fmt.Errorf("content is nil")
	}
	phased := false
	for _, part := range content.Parts {
		if part != nil && !part.Thought && part.Text != "" {
			m, _ := converters.MessageMetadataFromPart(part)
			phased = phased || m.Phase != ""
		}
	}
	var messageID string
	var texts []string
	for _, part := range content.Parts {
		if part == nil || part.Thought || part.Text == "" {
			continue
		}
		if phased {
			m, _ := converters.MessageMetadataFromPart(part)
			if m.Phase == converters.MessagePhaseCommentary {
				continue
			}
			if m.Phase != converters.MessagePhaseFinalAnswer {
				return nil, fmt.Errorf("structured output contains text without a final-answer phase")
			}
			if messageID != "" && m.ID != "" && messageID != m.ID {
				return nil, fmt.Errorf("structured output contains multiple final-answer messages")
			}
			if m.ID != "" {
				messageID = m.ID
			}
		}
		texts = append(texts, part.Text)
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("no structured response text found")
	}
	candidates := []string{strings.Join(texts, "")}
	if !phased && len(texts) > 1 {
		candidates = append(candidates, texts...)
	}
	return candidates, nil
}
