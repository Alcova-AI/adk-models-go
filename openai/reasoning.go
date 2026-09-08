// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
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

func applyReasoning(params *responses.ResponseNewParams, cfg *genai.GenerateContentConfig, defaults reasoningConfig) error {
	var thinking *genai.ThinkingConfig
	if cfg != nil {
		thinking = cfg.ThinkingConfig
	}
	resolved, err := defaults.resolve(thinking)
	if err != nil {
		return err
	}
	f := defaults.Family
	if f == "" {
		f = family.OpenAI
	}
	if f != family.OpenAI {
		return nil
	} // Cross-family Vercel options own the mapping.
	mapped, err := family.Map(f, resolved.ThinkingLevel)
	if err != nil {
		return err
	}
	var summary shared.ReasoningSummary
	if resolved.IncludeThoughts {
		summary = defaults.OpenAI.Summary
		if summary == "" {
			summary = shared.ReasoningSummaryAuto
		}
	}
	params.Reasoning = shared.ReasoningParam{Context: defaults.OpenAI.Context, Effort: shared.ReasoningEffort(mapped.Effort), Mode: defaults.OpenAI.Mode, Summary: summary}
	params.Include = appendUniqueInclude(params.Include, responses.ResponseIncludableReasoningEncryptedContent)
	return nil
}

func appendUniqueInclude(includes []responses.ResponseIncludable, value responses.ResponseIncludable) []responses.ResponseIncludable {
	for _, include := range includes {
		if include == value {
			return includes
		}
	}
	return append(includes, value)
}
