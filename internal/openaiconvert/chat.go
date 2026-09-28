// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package converters

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// LLMRequestToChatParams translates the common ADK contract without Responses-only state.
func LLMRequestToChatParams(name string, req *model.LLMRequest, maxTokens int) (sdk.ChatCompletionNewParams, error) {
	p := sdk.ChatCompletionNewParams{Model: name, MaxCompletionTokens: param.NewOpt(int64(maxTokens)), Store: param.NewOpt(false)}
	if req == nil {
		return p, ErrRequestNil
	}
	cfg := req.Config
	if cfg != nil {
		if err := chatGenerationConfig(&p, cfg); err != nil {
			return p, err
		}
		if cfg.SystemInstruction != nil {
			text, err := flattenContentText(cfg.SystemInstruction)
			if err != nil {
				return p, err
			}
			p.Messages = append(p.Messages, sdk.SystemMessage(text))
		}
		tools, err := convertTools(cfg)
		if err != nil {
			return p, err
		}
		for _, tool := range tools {
			f := tool.OfFunction
			p.Tools = append(p.Tools, sdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name: f.Name, Description: f.Description, Parameters: f.Parameters}))
		}
		if err := chatToolChoice(&p, cfg.ToolConfig); err != nil {
			return p, err
		}
	}
	tracker := callTracker{}
	for _, content := range req.Contents {
		if content == nil {
			continue
		}
		role := content.Role
		if role == "" {
			role = "user"
		}
		if role != "user" && role != "model" {
			return p, fmt.Errorf("openai chat: unsupported role %q", role)
		}
		if role == "model" && len(tracker.pending) > 0 {
			return p, fmt.Errorf("openai chat: missing tool results before next assistant message")
		}
		var texts strings.Builder
		var parts []sdk.ChatCompletionContentPartUnionParam
		var calls []sdk.ChatCompletionMessageToolCallUnionParam
		var results []sdk.ChatCompletionMessageParamUnion
		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Thought || len(part.ThoughtSignature) > 0 {
				return p, fmt.Errorf("openai chat: reasoning history is unsupported; remove it before switching APIs")
			}
			switch {
			case part.FunctionCall != nil:
				if role != "model" {
					return p, fmt.Errorf("openai chat: function calls require model role")
				}
				call, err := tracker.newFunctionCall(part.FunctionCall)
				if err != nil {
					return p, err
				}
				calls = append(calls, sdk.ChatCompletionMessageToolCallUnionParam{OfFunction: &sdk.ChatCompletionMessageFunctionToolCallParam{ID: call.CallID, Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{Name: call.Name, Arguments: call.Arguments}}})
			case part.FunctionResponse != nil:
				if role != "user" || len(part.FunctionResponse.Parts) > 0 {
					return p, fmt.Errorf("openai chat: tool results require user role and text-only output")
				}
				result, err := tracker.newFunctionResponse(part.FunctionResponse)
				if err != nil {
					return p, err
				}
				results = append(results, sdk.ToolMessage(result.Output.OfString.Value, result.CallID.Value))
			case part.Text != "":
				texts.WriteString(part.Text)
				parts = append(parts, sdk.TextContentPart(part.Text))
			case part.InlineData != nil || part.FileData != nil:
				if role != "user" {
					return p, fmt.Errorf("openai chat: media requires user role")
				}
				media, err := chatMedia(part)
				if err != nil {
					return p, err
				}
				parts = append(parts, media)
			default:
				return p, fmt.Errorf("openai chat: unsupported content part")
			}
		}
		if len(results) > 0 {
			if len(parts) > 0 || len(calls) > 0 {
				return p, fmt.Errorf("openai chat: tool results must be in a separate content entry")
			}
			p.Messages = append(p.Messages, results...)
			continue
		}
		if len(parts) == 0 && len(calls) == 0 {
			continue
		}
		if len(tracker.pending) > 0 && len(calls) == 0 {
			return p, fmt.Errorf("openai chat: missing tool results before next message")
		}
		if role == "model" {
			msg := sdk.AssistantMessage(texts.String())
			msg.OfAssistant.ToolCalls = calls
			p.Messages = append(p.Messages, msg)
		} else {
			p.Messages = append(p.Messages, sdk.UserMessage(parts))
		}
	}
	if len(tracker.pending) > 0 {
		return p, fmt.Errorf("openai chat: missing tool results")
	}
	if len(p.Messages) == 0 {
		return p, fmt.Errorf("openai chat: messages are empty")
	}
	return p, nil
}

func chatMedia(part *genai.Part) (sdk.ChatCompletionContentPartUnionParam, error) {
	var mediaErr error
	var result sdk.ChatCompletionContentPartUnionParam
	if part.InlineData != nil {
		media, err := inputMediaFromBytes(part.InlineData.Data, part.InlineData.MIMEType, part.InlineData.DisplayName)
		mediaErr = err
		if media.OfInputImage != nil {
			result = sdk.ImageContentPart(sdk.ChatCompletionContentPartImageImageURLParam{URL: media.OfInputImage.ImageURL.Value})
		}
		if media.OfInputFile != nil {
			f := media.OfInputFile
			result.OfFile = &sdk.ChatCompletionContentPartFileParam{File: sdk.ChatCompletionContentPartFileFileParam{FileData: f.FileData, Filename: f.Filename}}
		}
	} else {
		media, err := inputMediaFromURI(part.FileData.FileURI, part.FileData.MIMEType, part.FileData.DisplayName)
		mediaErr = err
		if media.OfInputImage != nil {
			if media.OfInputImage.FileID.Value != "" {
				return result, fmt.Errorf("openai chat: image file IDs are unsupported; supply image bytes or a URL")
			}
			result = sdk.ImageContentPart(sdk.ChatCompletionContentPartImageImageURLParam{URL: media.OfInputImage.ImageURL.Value})
		}
		if media.OfInputFile != nil {
			if media.OfInputFile.FileURL.Value != "" {
				return result, fmt.Errorf("openai chat: file URLs are unsupported; supply file bytes or an uploaded file ID")
			}
			result.OfFile = &sdk.ChatCompletionContentPartFileParam{File: sdk.ChatCompletionContentPartFileFileParam{FileID: media.OfInputFile.FileID}}
		}
	}
	return result, mediaErr
}

func chatGenerationConfig(p *sdk.ChatCompletionNewParams, c *genai.GenerateContentConfig) error {
	if c.TopK != nil || c.CandidateCount > 1 || c.Labels != nil || c.SafetySettings != nil || c.ResponseLogprobs || c.Logprobs != nil {
		return fmt.Errorf("openai chat: topK, multiple candidates, labels, safety settings and log probabilities are unsupported")
	}
	if c.ResponseMIMEType != "" && c.ResponseMIMEType != "text/plain" && c.ResponseMIMEType != "application/json" {
		return ErrUnsupportedMIMEType
	}
	if c.MaxOutputTokens < 0 {
		return fmt.Errorf("openai chat: max output tokens must not be negative")
	}
	if c.MaxOutputTokens > 0 {
		p.MaxCompletionTokens = param.NewOpt(int64(c.MaxOutputTokens))
	}
	if c.Temperature != nil {
		p.Temperature = param.NewOpt(float64(*c.Temperature))
	}
	if c.TopP != nil {
		p.TopP = param.NewOpt(float64(*c.TopP))
	}
	if c.FrequencyPenalty != nil {
		p.FrequencyPenalty = param.NewOpt(float64(*c.FrequencyPenalty))
	}
	if c.PresencePenalty != nil {
		p.PresencePenalty = param.NewOpt(float64(*c.PresencePenalty))
	}
	if c.Seed != nil {
		p.Seed = param.NewOpt(int64(*c.Seed))
	}
	if len(c.StopSequences) > 0 {
		p.Stop.OfStringArray = append([]string(nil), c.StopSequences...)
	}
	if c.ResponseSchema != nil || c.ResponseJsonSchema != nil {
		f, err := newJSONSchemaFormat(c)
		if err != nil {
			return err
		}
		p.ResponseFormat.OfJSONSchema = &shared.ResponseFormatJSONSchemaParam{JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{Name: f.Name, Schema: f.Schema, Strict: f.Strict}}
	} else if c.ResponseMIMEType == "application/json" {
		f := shared.NewResponseFormatJSONObjectParam()
		p.ResponseFormat.OfJSONObject = &f
	}
	return nil
}

func chatToolChoice(p *sdk.ChatCompletionNewParams, cfg *genai.ToolConfig) error {
	if cfg == nil || cfg.FunctionCallingConfig == nil {
		return nil
	}
	c := cfg.FunctionCallingConfig
	mode := "auto"
	switch c.Mode {
	case "", genai.FunctionCallingConfigModeUnspecified, genai.FunctionCallingConfigModeAuto:
	case genai.FunctionCallingConfigModeAny:
		mode = "required"
	case genai.FunctionCallingConfigModeNone:
		mode = "none"
	default:
		return fmt.Errorf("openai chat: unsupported tool calling mode %q", c.Mode)
	}
	if len(c.AllowedFunctionNames) > 0 {
		if mode == "none" {
			return fmt.Errorf("openai chat: allowed functions conflict with disabled tools")
		}
		filtered := make([]sdk.ChatCompletionToolUnionParam, 0, len(c.AllowedFunctionNames))
		for _, name := range c.AllowedFunctionNames {
			found := false
			for _, tool := range p.Tools {
				if tool.OfFunction.Function.Name == name {
					filtered = append(filtered, tool)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("openai chat: allowed function %q is not declared", name)
			}
		}
		p.Tools = filtered
	}
	if mode == "required" && len(p.Tools) == 0 {
		return fmt.Errorf("openai chat: required tool choice needs tools")
	}
	p.ToolChoice.OfAuto = param.NewOpt(mode)
	return nil
}

// ChatToLLMResponse converts the completed reply, including refusal and detailed usage.
func ChatToLLMResponse(reply *sdk.ChatCompletion) (*model.LLMResponse, error) {
	if reply == nil || len(reply.Choices) != 1 || reply.Choices[0].FinishReason == "" {
		return nil, fmt.Errorf("openai chat: expected one completed choice")
	}
	choice := reply.Choices[0]
	content := &genai.Content{Role: "model"}
	if choice.Message.Content != "" {
		content.Parts = append(content.Parts, &genai.Part{Text: choice.Message.Content})
	}
	if choice.Message.Refusal != "" {
		content.Parts = append(content.Parts, &genai.Part{Text: choice.Message.Refusal})
	}
	for _, call := range choice.Message.ToolCalls {
		if call.Type != "function" || call.ID == "" || call.Function.Name == "" {
			return nil, fmt.Errorf("openai chat: invalid function call")
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || args == nil {
			return nil, fmt.Errorf("openai chat: function arguments must be a JSON object")
		}
		content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Function.Name, Args: args}})
	}
	finish := genai.FinishReasonOther
	switch choice.FinishReason {
	case "stop", "tool_calls":
		finish = genai.FinishReasonStop
	case "length":
		finish = genai.FinishReasonMaxTokens
	case "content_filter":
		finish = genai.FinishReasonSafety
	}
	u := reply.Usage
	return &model.LLMResponse{Content: content, TurnComplete: true, FinishReason: finish, UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: safeInt32(u.PromptTokens), CandidatesTokenCount: safeInt32(u.CompletionTokens), TotalTokenCount: safeInt32(u.TotalTokens), CachedContentTokenCount: safeInt32(u.PromptTokensDetails.CachedTokens), ThoughtsTokenCount: safeInt32(u.CompletionTokensDetails.ReasoningTokens)}}, nil
}
