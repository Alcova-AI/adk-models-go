// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package openai projects OpenAI-specific options onto Vercel's provider-
// neutral Language Model V4 request.
package openai

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

type Options struct {
	PromptCaching adkmodels.OpenAIPromptCachingConfig
}

func (o Options) Apply(request *protocol.CallOptions) error {
	if err := o.PromptCaching.Validate(); err != nil {
		return err
	}
	request.ProviderOptions = cloneOptions(request.ProviderOptions)
	openAI := cloneValues(request.ProviderOptions["openai"])
	cache := o.PromptCaching
	if cache.Key != "" {
		openAI["promptCacheKey"] = cache.Key
	}
	if cache.Mode != adkmodels.OpenAIPromptCacheProviderDefault {
		openAI["promptCacheOptions"] = map[string]any{"mode": string(cache.Mode), "ttl": "30m"}
	}
	if len(openAI) > 0 {
		request.ProviderOptions["openai"] = openAI
	}
	if cache.Mode == adkmodels.OpenAIPromptCacheProviderDefault {
		return nil
	}
	remaining := 4
	if cache.Mode == adkmodels.OpenAIPromptCacheImplicit {
		remaining--
	}
	if (cache.SystemInstruction != nil || cache.Tools != nil) && markSystem(request) {
		remaining--
	}
	if cache.ConversationHistory != nil {
		markHistory(request, remaining)
	}
	return nil
}

func markSystem(request *protocol.CallOptions) bool {
	for i := range request.Prompt {
		if request.Prompt[i].Role != "system" {
			continue
		}
		request.Prompt[i].ProviderOptions = breakpointOptions(request.Prompt[i].ProviderOptions)
		return true
	}
	return false
}

func markHistory(request *protocol.CallOptions, limit int) int {
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
			parts[j].ProviderOptions = breakpointOptions(parts[j].ProviderOptions)
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
