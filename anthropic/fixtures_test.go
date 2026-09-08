// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkanthropic

import (
	"fmt"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// These fixture helpers port existing test scenarios to ModelConfig. They are
// test-only; the public API has one constructor and no legacy option surface.
type testConfig struct {
	Client         anthropic.Client
	CanonicalModel string
	RequestModel   string
}
type testOption func(*adkmodels.ModelConfig) error
type PromptCachingConfig = adkmodels.AnthropicPromptCachingConfig
type CacheBreakpoint = adkmodels.AnthropicCacheBreakpoint

const PromptCacheProviderDefault = adkmodels.AnthropicPromptCacheProviderDefault
const PromptCacheManual = adkmodels.AnthropicPromptCacheManual

type ReasoningConfig struct {
	DefaultLevel genai.ThinkingLevel
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
func withDefaultMaxTokens(n int) testOption {
	return func(c *adkmodels.ModelConfig) error {
		if n <= 0 {
			return fmt.Errorf("default max tokens must be positive")
		}
		c.DefaultMaxOutputTokens = int32(n)
		return nil
	}
}
func withReasoning(r ReasoningConfig) testOption {
	return func(c *adkmodels.ModelConfig) error { c.Reasoning.DefaultLevel = r.DefaultLevel; return nil }
}
func withPromptCaching(cache PromptCachingConfig) testOption {
	return func(c *adkmodels.ModelConfig) error {

		c.PromptCaching.Anthropic = cache
		return nil
	}
}
func withVercelGateway(v adkmodels.VercelConfig) testOption {
	return func(c *adkmodels.ModelConfig) error {

		c.Vercel = &v
		return nil
	}
}
