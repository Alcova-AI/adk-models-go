// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package adkmodels defines the shared contract for ADK model adapters.
package adkmodels

import (
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/genai"

	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/Alcova-AI/adk-models-go/toolschema"
)

const (
	ThinkingLevelXHigh genai.ThinkingLevel = family.XHigh
	ThinkingLevelMax   genai.ThinkingLevel = family.Max
)

type ModelConfig struct {
	ToolSchemas            toolschema.Config
	CanonicalModel         string
	RequestModel           string
	DefaultMaxOutputTokens int32
	Reasoning              ReasoningConfig
	PromptCaching          PromptCachingConfig
	Vercel                 *VercelConfig
}

// Validate checks identity and explicitly supplied settings. Endpoint support is
// left to the provider; the library does not maintain a model catalogue.
func (c ModelConfig) Validate() error {
	f, err := family.Detect(c.CanonicalModel)
	if err != nil {
		return err
	}
	if c.RequestModel != "" {
		if err := family.ValidateRequest(f, c.RequestModel); err != nil {
			return err
		}
	}
	if c.DefaultMaxOutputTokens < 0 {
		return fmt.Errorf("default max output tokens must not be negative")
	}
	if !family.ValidLevel(c.Reasoning.DefaultLevel) {
		return fmt.Errorf("unsupported default thinking level %q", c.Reasoning.DefaultLevel)
	}
	if err := c.Reasoning.OpenAI.Validate(); err != nil {
		return err
	}
	if c.Vercel != nil {
		return c.Vercel.Validate()
	}
	return nil
}

type ReasoningConfig struct {
	DefaultLevel genai.ThinkingLevel
	OpenAI       OpenAIReasoningConfig
}

// OpenAIReasoningConfig preserves the SDK controls not represented by GenAI.
// Summary is used only when IncludeThoughts is true.
type OpenAIReasoningConfig struct {
	Context shared.ReasoningContext
	Mode    shared.ReasoningMode
	Summary shared.ReasoningSummary
}

func (c OpenAIReasoningConfig) Validate() error {
	switch c.Context {
	case "", shared.ReasoningContextAuto, shared.ReasoningContextCurrentTurn, shared.ReasoningContextAllTurns:
	default:
		return fmt.Errorf("unsupported reasoning context %q", c.Context)
	}
	switch c.Mode {
	case "", shared.ReasoningModeStandard, shared.ReasoningModePro:
	default:
		return fmt.Errorf("unsupported reasoning mode %q", c.Mode)
	}
	switch c.Summary {
	case "", shared.ReasoningSummaryAuto, shared.ReasoningSummaryConcise, shared.ReasoningSummaryDetailed:
	default:
		return fmt.Errorf("unsupported reasoning summary %q", c.Summary)
	}
	return nil
}

type PromptCachingConfig struct {
	Anthropic AnthropicPromptCachingConfig
	OpenAI    OpenAIPromptCachingConfig
}

type AnthropicPromptCacheMode uint8

const (
	AnthropicPromptCacheProviderDefault AnthropicPromptCacheMode = iota
	AnthropicPromptCacheManual
)

type AnthropicCacheBreakpoint struct {
	TTL anthropic.CacheControlEphemeralTTL
}
type AnthropicPromptCachingConfig struct {
	Mode                AnthropicPromptCacheMode
	Auto                *AnthropicCacheBreakpoint
	SystemInstruction   *AnthropicCacheBreakpoint
	Tools               *AnthropicCacheBreakpoint
	ConversationHistory *AnthropicCacheBreakpoint
}

func (c AnthropicPromptCachingConfig) Validate() error {
	if c.Mode > AnthropicPromptCacheManual {
		return fmt.Errorf("unsupported Anthropic prompt cache mode %d", c.Mode)
	}
	if c.Mode != AnthropicPromptCacheManual && (c.Auto != nil || c.SystemInstruction != nil || c.Tools != nil || c.ConversationHistory != nil) {
		return fmt.Errorf("prompt cache breakpoints require manual mode")
	}
	return nil
}

type OpenAIPromptCacheMode string

const (
	OpenAIPromptCacheProviderDefault OpenAIPromptCacheMode = ""
	OpenAIPromptCacheImplicit        OpenAIPromptCacheMode = "implicit"
	OpenAIPromptCacheExplicit        OpenAIPromptCacheMode = "explicit"
)

type OpenAICacheBreakpoint struct{}
type OpenAIPromptCachingConfig struct {
	Mode                OpenAIPromptCacheMode
	Key                 string
	SystemInstruction   *OpenAICacheBreakpoint
	Tools               *OpenAICacheBreakpoint
	ConversationHistory *OpenAICacheBreakpoint
}

func (c OpenAIPromptCachingConfig) Validate() error {
	switch c.Mode {
	case OpenAIPromptCacheProviderDefault, OpenAIPromptCacheImplicit, OpenAIPromptCacheExplicit:
	default:
		return fmt.Errorf("unsupported OpenAI prompt caching mode %q", c.Mode)
	}
	if c.SystemInstruction != nil && c.Tools != nil {
		return fmt.Errorf("SystemInstruction and Tools select the same OpenAI cache boundary; set only one")
	}
	if c.Mode == OpenAIPromptCacheProviderDefault && (c.SystemInstruction != nil || c.Tools != nil || c.ConversationHistory != nil) {
		return fmt.Errorf("explicit cache breakpoints require implicit or explicit prompt caching mode")
	}
	if len(c.Key) > 64 {
		return fmt.Errorf("prompt cache key must be at most 64 characters")
	}
	return nil
}
