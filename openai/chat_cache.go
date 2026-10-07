// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
)

func applyChatPromptCaching(params *sdk.ChatCompletionNewParams, cfg adkmodels.OpenAIPromptCachingConfig, selected bool) {
	if cfg.Mode == adkmodels.OpenAIPromptCacheProviderDefault {
		return
	}
	params.PromptCacheOptions = sdk.ChatCompletionNewParamsPromptCacheOptions{Mode: string(cfg.Mode), Ttl: "30m"}
	available := 4
	if cfg.Mode == adkmodels.OpenAIPromptCacheImplicit {
		available--
	}
	if cfg.SystemInstruction != nil || cfg.Tools != nil {
		for i := range params.Messages {
			system := params.Messages[i].OfSystem
			if system == nil {
				continue
			}
			parts := system.Content.OfArrayOfContentParts
			if len(parts) == 0 && system.Content.OfString.Value != "" {
				parts = []sdk.ChatCompletionContentPartTextParam{*sdk.TextContentPart(system.Content.OfString.Value).OfText}
				system.Content = sdk.ChatCompletionSystemMessageParamContentUnion{OfArrayOfContentParts: parts}
			}
			if len(parts) > 0 {
				index := len(parts) - 1
				if selected {
					index = 0
				}
				parts[index].PromptCacheBreakpoint = sdk.NewChatCompletionContentPartTextPromptCacheBreakpointParam()
				available--
			}
			break
		}
	}
	if cfg.ConversationHistory == nil {
		return
	}
	for i := len(params.Messages) - 1; i >= 0 && available > 0; i-- {
		user := params.Messages[i].OfUser
		if user == nil {
			continue
		}
		parts := user.Content.OfArrayOfContentParts
		if len(parts) == 0 && user.Content.OfString.Value != "" {
			parts = []sdk.ChatCompletionContentPartUnionParam{sdk.TextContentPart(user.Content.OfString.Value)}
			user.Content = sdk.ChatCompletionUserMessageParamContentUnion{OfArrayOfContentParts: parts}
		}
		eligible := len(parts) > 0
		for _, part := range parts {
			if part.OfText == nil {
				eligible = false
			}
		}
		if !eligible {
			continue
		}
		parts[len(parts)-1].OfText.PromptCacheBreakpoint = sdk.NewChatCompletionContentPartTextPromptCacheBreakpointParam()
		available--
	}
}
