// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package family

import (
	"fmt"

	"google.golang.org/genai"
)

// Resolve applies caller defaults without changing the request or its config.
func Resolve(defaultLevel genai.ThinkingLevel, cfg *genai.ThinkingConfig) (genai.ThinkingLevel, bool, error) {
	level := defaultLevel
	include := false
	if cfg != nil {
		if cfg.ThinkingBudget != nil {
			return "", false, fmt.Errorf("ThinkingBudget is not supported; use ThinkingLevel")
		}
		include = cfg.IncludeThoughts
		if cfg.ThinkingLevel != "" && cfg.ThinkingLevel != genai.ThinkingLevelUnspecified {
			level = cfg.ThinkingLevel
		}
	}
	if !ValidLevel(level) {
		return "", false, fmt.Errorf("unsupported thinking level %q", level)
	}
	return level, include, nil
}
