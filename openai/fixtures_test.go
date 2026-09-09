// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	"fmt"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// These fixture helpers port existing test scenarios to ModelConfig. They are
// test-only; the public API has one constructor and no legacy option surface.
type testConfig struct {
	Client         openai.Client
	CanonicalModel string
	RequestModel   string
}
type testOption func(*adkmodels.ModelConfig) error
type PromptCachingConfig = adkmodels.OpenAIPromptCachingConfig
type CacheBreakpoint = adkmodels.OpenAICacheBreakpoint

const PromptCacheProviderDefault = adkmodels.OpenAIPromptCacheProviderDefault
const PromptCacheImplicit = adkmodels.OpenAIPromptCacheImplicit
const PromptCacheExplicit = adkmodels.OpenAIPromptCacheExplicit
const ThinkingLevelXHigh = adkmodels.ThinkingLevelXHigh

type ReasoningConfig struct {
	DefaultLevel genai.ThinkingLevel
	Context      shared.ReasoningContext
	Mode         shared.ReasoningMode
	Summary      shared.ReasoningSummary
}

func newTestModel(cfg testConfig, opts ...testOption) (model.LLM, error) {
	m := adkmodels.ModelConfig{CanonicalModel: cfg.CanonicalModel, RequestModel: cfg.RequestModel}
	for _, apply := range opts {
		if apply == nil {
			return nil, fmt.Errorf("nil test option")
		}
		if err := apply(&m); err != nil {
			return nil, err
		}
	}
	return NewModel(Config{Client: cfg.Client, Model: m})
}
func withReasoning(r ReasoningConfig) testOption {
	return func(c *adkmodels.ModelConfig) error {
		c.Reasoning.DefaultLevel = r.DefaultLevel
		c.Reasoning.OpenAI = adkmodels.OpenAIReasoningConfig{Context: r.Context, Mode: r.Mode, Summary: r.Summary}
		return nil
	}
}
func withPromptCaching(cache PromptCachingConfig) testOption {
	return func(c *adkmodels.ModelConfig) error {

		c.PromptCaching.OpenAI = cache
		return nil
	}
}
func withVercelGateway(v adkmodels.VercelConfig) testOption {
	return func(c *adkmodels.ModelConfig) error {

		c.Vercel = &v
		return nil
	}
}
