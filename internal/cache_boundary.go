// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package internal

import (
	"fmt"
	"google.golang.org/adk/v2/model"
	"strings"
)

// SystemInstructionTexts returns a shared prefix and optional suffix without
// modifying the caller's request. A nil boundary retains legacy conversion.
func SystemInstructionTexts(req *model.LLMRequest, index *int) ([]string, error) {
	if index == nil {
		return nil, nil
	}
	if req == nil || req.Config == nil || req.Config.SystemInstruction == nil {
		return nil, fmt.Errorf("cache boundary requires a system instruction")
	}
	parts := req.Config.SystemInstruction.Parts
	if *index < 0 || *index >= len(parts) {
		return nil, fmt.Errorf("system cache part index %d is out of range", *index)
	}
	var prefix, suffix strings.Builder
	for i, part := range parts {
		if part == nil {
			continue
		}
		if part.Text == "" || part.Thought || part.InlineData != nil || part.FileData != nil || part.FunctionCall != nil || part.FunctionResponse != nil {
			return nil, fmt.Errorf("system cache boundary requires text-only parts")
		}
		if i <= *index {
			prefix.WriteString(part.Text)
		} else {
			suffix.WriteString(part.Text)
		}
	}
	if prefix.Len() == 0 {
		return nil, fmt.Errorf("system cache boundary selects an empty prefix")
	}
	result := []string{prefix.String()}
	if suffix.Len() > 0 {
		result = append(result, suffix.String())
	}
	return result, nil
}
