// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

func convertGenerateResult(result protocol.GenerateResult, includeThoughts bool) (*model.LLMResponse, error) {
	parts := make([]*genai.Part, 0, len(result.Content))
	for _, output := range result.Content {
		part, err := convertOutputPart(output, includeThoughts)
		if err != nil {
			return nil, err
		}
		if part != nil {
			parts = append(parts, part)
		}
	}
	response := &model.LLMResponse{
		Content:      &genai.Content{Role: string(genai.RoleModel), Parts: parts},
		FinishReason: finishReason(result.FinishReason), UsageMetadata: usageMetadata(result.Usage),
		TurnComplete: true,
	}
	if result.Response != nil {
		response.ModelVersion = result.Response.ModelID
	}
	attachMetadata(response, result.ProviderMetadata, result.Usage)
	if result.Response != nil {
		parsed, _ := adkmodels.MetadataFromResponse(response)
		parsed.ResponseID = result.Response.ID
		metadata.Attach(response, parsed)
	}
	return response, nil
}

func convertOutputPart(output protocol.OutputPart, includeThoughts bool) (*genai.Part, error) {
	switch output.Type {
	case "text":
		return &genai.Part{Text: output.Text}, nil
	case "reasoning":
		signature, err := encodeReasoningPart(output)
		if err != nil {
			return nil, err
		}
		part := &genai.Part{Thought: true, ThoughtSignature: signature}
		if includeThoughts {
			part.Text = output.Text
		}
		return part, nil
	case "tool-call":
		args := map[string]any{}
		if output.Input != "" {
			if err := json.Unmarshal([]byte(output.Input), &args); err != nil {
				return nil, fmt.Errorf("vercel: parse tool call %q input: %w", output.ToolName, err)
			}
		}
		return &genai.Part{FunctionCall: &genai.FunctionCall{ID: output.ToolCallID, Name: output.ToolName, Args: args}}, nil
	case "tool-result":
		return &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: output.ToolCallID, Name: output.ToolName, Response: map[string]any{"result": output.Result}}}, nil
	case "file", "reasoning-file":
		return outputFile(output)
	case "source", "custom", "tool-approval-request":
		return nil, nil
	default:
		return nil, fmt.Errorf("vercel: unsupported Language Model V4 content type %q", output.Type)
	}
}

func outputFile(output protocol.OutputPart) (*genai.Part, error) {
	if output.Data == nil {
		return nil, fmt.Errorf("vercel: output file has no data")
	}
	switch output.Data.Type {
	case "data":
		data, err := base64.StdEncoding.DecodeString(output.Data.Data)
		if err != nil {
			return nil, fmt.Errorf("vercel: decode output file: %w", err)
		}
		return &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: output.MediaType}}, nil
	case "url":
		return &genai.Part{FileData: &genai.FileData{FileURI: output.Data.URL, MIMEType: output.MediaType}}, nil
	default:
		return nil, fmt.Errorf("vercel: unsupported output file data type %q", output.Data.Type)
	}
}

func finishReason(reason protocol.FinishReason) genai.FinishReason {
	switch reason.Unified {
	case "stop", "tool-calls":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "content-filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonOther
	}
}

func usageMetadata(usage protocol.Usage) *genai.GenerateContentResponseUsageMetadata {
	input := valueOrZero(usage.InputTokens.Total)
	output := valueOrZero(usage.OutputTokens.Total)
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount: safeInt32(input), CandidatesTokenCount: safeInt32(output),
		TotalTokenCount: safeInt32(input + output), CachedContentTokenCount: safeInt32(valueOrZero(usage.InputTokens.CacheRead)),
		ThoughtsTokenCount:      safeInt32(valueOrZero(usage.OutputTokens.Reasoning)),
		PromptTokensDetails:     []*genai.ModalityTokenCount{{Modality: genai.MediaModalityText, TokenCount: safeInt32(input)}},
		CandidatesTokensDetails: []*genai.ModalityTokenCount{{Modality: genai.MediaModalityText, TokenCount: safeInt32(output)}},
	}
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func safeInt32(value int64) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}

func attachMetadata(response *model.LLMResponse, providerMetadata map[string]any, usage protocol.Usage) {
	if response == nil {
		return
	}
	parsed, ok := responseMetadata(providerMetadata, usage)
	if !ok {
		return
	}
	if response.CustomMetadata == nil {
		response.CustomMetadata = map[string]any{}
	}
	metadata.Attach(response, parsed)
}
