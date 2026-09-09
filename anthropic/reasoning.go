// Copyright 2025 Alcova AI
// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkanthropic

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"
)

type reasoningConfig struct {
	DefaultLevel genai.ThinkingLevel
	OpenAI       adkmodels.OpenAIReasoningConfig
	Family       family.Family
}
type resolvedReasoning struct {
	ThinkingLevel   genai.ThinkingLevel
	IncludeThoughts bool
}

func (c reasoningConfig) resolve(cfg *genai.ThinkingConfig) (resolvedReasoning, error) {
	level, include, err := family.Resolve(c.DefaultLevel, cfg)
	return resolvedReasoning{level, include}, err
}

type thinkingMapping struct {
	Thinking anthropic.ThinkingConfigParamUnion
	Effort   anthropic.OutputConfigEffort
}

func (c reasoningConfig) mapThinking(cfg *genai.ThinkingConfig) (thinkingMapping, error) {
	resolved, err := c.resolve(cfg)
	if err != nil {
		return thinkingMapping{}, err
	}
	f := c.Family
	if f == "" {
		f = family.Anthropic
	}
	if f != family.Anthropic {
		return thinkingMapping{}, nil
	}
	mapped, err := family.Map(f, resolved.ThinkingLevel)
	if err != nil {
		return thinkingMapping{}, err
	}
	result := thinkingMapping{Effort: anthropic.OutputConfigEffort(mapped.Effort)}
	switch mapped.Thinking {
	case "disabled":
		result.Thinking.OfDisabled = &anthropic.ThinkingConfigDisabledParam{}
	case "adaptive":
		display := anthropic.ThinkingConfigAdaptiveDisplayOmitted
		if resolved.IncludeThoughts {
			display = anthropic.ThinkingConfigAdaptiveDisplaySummarized
		}
		result.Thinking.OfAdaptive = &anthropic.ThinkingConfigAdaptiveParam{Display: display}
	}
	return result, nil
}
