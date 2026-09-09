// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"
	"github.com/openai/openai-go/v3/responses"
	"google.golang.org/adk/v2/model"
)

func attachOpenAIResponseMetadata(resp *model.LLMResponse, openAIResp *responses.Response) {
	if resp == nil || openAIResp == nil {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = make(map[string]any)
	}
	metadata.Attach(resp, adkmodels.Metadata{
		ResponseID:            openAIResp.ID,
		CacheWriteInputTokens: openAIResp.Usage.InputTokensDetails.CacheWriteTokens,
	})
}
