// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkanthropic

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"
	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/adk/v2/model"
)

func attachAnthropicResponseMetadata(resp *model.LLMResponse, msg *anthropic.Message) {
	if resp == nil || msg == nil {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = make(map[string]any)
	}
	metadata.Attach(resp, adkmodels.Metadata{
		ResponseID:                 msg.ID,
		CacheWriteInputTokens:      msg.Usage.CacheCreationInputTokens,
		CacheCreation5mInputTokens: msg.Usage.CacheCreation.Ephemeral5mInputTokens,
		CacheCreation1hInputTokens: msg.Usage.CacheCreation.Ephemeral1hInputTokens,
	})
}
