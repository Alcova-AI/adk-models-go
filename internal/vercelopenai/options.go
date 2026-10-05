// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package openai projects OpenAI-specific options onto Vercel's provider-
// neutral Language Model V4 request.
package openai

import (
	"fmt"
	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

type Options struct {
	PromptCaching                   adkmodels.OpenAIPromptCachingConfig
	SystemInstructionCacheOptions   protocol.ProviderOptions
	ConversationHistoryCacheOptions protocol.ProviderOptions
}

func (o Options) Apply(request *protocol.CallOptions) error {
	if err := o.PromptCaching.Validate(); err != nil {
		return err
	}
	request.ProviderOptions = cloneOptions(request.ProviderOptions)
	openAI := cloneValues(request.ProviderOptions["openai"])
	cache := o.PromptCaching
	if cache.Key != "" {
		if _, exists := openAI["promptCacheKey"]; exists {
			return fmt.Errorf("openai.promptCacheKey conflicts with typed caching")
		}
		openAI["promptCacheKey"] = cache.Key
	}
	if cache.Mode != adkmodels.OpenAIPromptCacheProviderDefault {
		if _, exists := openAI["promptCacheOptions"]; exists {
			return fmt.Errorf("openai.promptCacheOptions conflicts with typed caching")
		}
		openAI["promptCacheOptions"] = map[string]any{"mode": string(cache.Mode), "ttl": "30m"}
	}
	if len(openAI) > 0 {
		request.ProviderOptions["openai"] = openAI
	}
	if cache.Mode == adkmodels.OpenAIPromptCacheProviderDefault {
		if len(o.SystemInstructionCacheOptions) > 0 {
			markSystem(request, o.SystemInstructionCacheOptions)
		}
		if len(o.ConversationHistoryCacheOptions) > 0 {
			markHistory(request, 2, o.ConversationHistoryCacheOptions)
		}
		return nil
	}
	remaining := 4
	if cache.Mode == adkmodels.OpenAIPromptCacheImplicit {
		remaining--
	}
	if (cache.SystemInstruction != nil || cache.Tools != nil) && markSystem(request, o.SystemInstructionCacheOptions) {
		remaining--
	}
	if cache.ConversationHistory != nil {
		markHistory(request, remaining, o.ConversationHistoryCacheOptions)
	}
	return nil
}

func markSystem(request *protocol.CallOptions, options protocol.ProviderOptions) bool {
	for i := range request.Prompt {
		if request.Prompt[i].Role != "system" {
			continue
		}
		request.Prompt[i].ProviderOptions = mergeBreakpointOptions(request.Prompt[i].ProviderOptions, options)
		return true
	}
	return false
}

func markHistory(request *protocol.CallOptions, limit int, options protocol.ProviderOptions) int {
	marked := 0
	for i := len(request.Prompt) - 1; i >= 0 && marked < limit; i-- {
		message := &request.Prompt[i]
		if message.Role != "user" {
			continue
		}
		parts, ok := message.Content.([]protocol.Part)
		if !ok || hasFile(parts) {
			continue
		}
		for j := len(parts) - 1; j >= 0; j-- {
			if parts[j].Type != "text" {
				continue
			}
			parts[j].ProviderOptions = mergeBreakpointOptions(parts[j].ProviderOptions, options)
			message.Content = parts
			marked++
			break
		}
	}
	return marked
}

func hasFile(parts []protocol.Part) bool {
	for _, part := range parts {
		if part.Type == "file" {
			return true
		}
	}
	return false
}

func mergeBreakpointOptions(source, options protocol.ProviderOptions) protocol.ProviderOptions {
	if len(options) == 0 {
		return breakpointOptions(source)
	}
	result := cloneOptions(source)
	for namespace, values := range options {
		merged := cloneValues(result[namespace])
		for key, value := range values {
			merged[key] = value
		}
		result[namespace] = merged
	}
	return result
}

func breakpointOptions(source protocol.ProviderOptions) protocol.ProviderOptions {
	result := cloneOptions(source)
	values := cloneValues(result["openai"])
	values["promptCacheBreakpoint"] = map[string]any{"mode": "explicit"}
	result["openai"] = values
	return result
}

func cloneOptions(source protocol.ProviderOptions) protocol.ProviderOptions {
	result := make(protocol.ProviderOptions, len(source)+2)
	for key, values := range source {
		result[key] = cloneValues(values)
	}
	return result
}

func cloneValues(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+2)
	for key, value := range source {
		result[key] = value
	}
	return result
}
