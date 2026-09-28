// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	"context"
	"fmt"
	"iter"
	"regexp"
	"strings"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal"
	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/Alcova-AI/adk-models-go/internal/gateway"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"
	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type chatModel struct {
	client             sdk.Client
	name, requestModel string
	maxTokens          int
	schemas            *toolschema.Processor
	reasoning          reasoningConfig
	caching            adkmodels.OpenAIPromptCachingConfig
	vercel             *adkmodels.VercelConfig
}

func newChatModel(cfg Config) (model.LLM, error) {
	if len(cfg.Client.Options) == 0 {
		return nil, fmt.Errorf("client must be constructed with openai.NewClient")
	}
	if err := cfg.Model.Validate(); err != nil {
		return nil, err
	}
	f, err := family.Detect(cfg.Model.CanonicalModel)
	if err != nil {
		return nil, err
	}
	if f != family.OpenAI && f != family.Compatible && cfg.Model.Vercel == nil {
		return nil, fmt.Errorf("direct openai chat adapter requires its own model family; cross-family requests require Vercel")
	}
	if cfg.Model.Reasoning.OpenAI != (adkmodels.OpenAIReasoningConfig{}) {
		return nil, fmt.Errorf("openai chat: reasoning context, mode and summary settings require Responses")
	}
	cache := cfg.Model.PromptCaching.OpenAI
	if err := cache.Validate(); err != nil {
		return nil, err
	}
	if cache.Mode != adkmodels.OpenAIPromptCacheProviderDefault || cache.SystemInstruction != nil || cache.Tools != nil || cache.ConversationHistory != nil {
		return nil, fmt.Errorf("openai chat: explicit cache modes and breakpoints are unsupported")
	}
	if f != family.OpenAI && cache.Key != "" {
		return nil, fmt.Errorf("openai chat: OpenAI cache keys require an OpenAI model")
	}
	requestModel := cfg.Model.RequestModel
	if requestModel == "" {
		requestModel = cfg.Model.CanonicalModel
	}
	tokens := int(cfg.Model.DefaultMaxOutputTokens)
	if tokens == 0 {
		tokens = defaultMaxTokens
	}
	route := "chat"
	if cfg.Model.Vercel != nil {
		route = "vercel-openai-chat"
	}
	return &chatModel{client: cfg.Client, name: cfg.Model.CanonicalModel, requestModel: requestModel, maxTokens: tokens,
		schemas:   toolschema.New(cfg.Model.ToolSchemas, toolschema.Target{Provider: string(f), Route: route}),
		reasoning: reasoningConfig{DefaultLevel: cfg.Model.Reasoning.DefaultLevel, Family: f}, caching: cache, vercel: gateway.CloneConfig(cfg.Model.Vercel)}, nil
}

func (m *chatModel) Name() string { return m.name }

func (m *chatModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return internal.GenerateWithTimeout(ctx, req, func(ctx context.Context) iter.Seq2[*model.LLMResponse, error] { return m.generate(ctx, req, stream) })
}

func (m *chatModel) generate(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	params, err := converters.LLMRequestToChatParams(m.requestModel, req, m.maxTokens)
	if err != nil {
		return singleErrorSequence(err)
	}
	prepared, err := m.schemas.Prepare(ctx, toolschema.Tools(req))
	if err != nil {
		return singleErrorSequence(err)
	}
	for i := range params.Tools {
		f := &params.Tools[i].OfFunction.Function
		schema := prepared[f.Name]
		f.Parameters = nil
		f.SetExtraFields(map[string]any{"parameters": schema.JSONSchema})
		if schema.Strict != nil {
			f.Strict = param.NewOpt(*schema.Strict)
		}
	}
	var thinking *genai.ThinkingConfig
	if req.Config != nil {
		thinking = req.Config.ThinkingConfig
	}
	resolved, err := m.reasoning.resolve(thinking)
	if err != nil {
		return singleErrorSequence(err)
	}
	if resolved.IncludeThoughts {
		return singleErrorSequence(fmt.Errorf("openai chat: reasoning output is unsupported; use Responses"))
	}
	// Unmapped compatible endpoints use the original Chat Completions token field.
	// Do not send OpenAI-specific storage or reasoning controls.
	if m.reasoning.Family == family.Compatible {
		if resolved.ThinkingLevel != "" && resolved.ThinkingLevel != genai.ThinkingLevelUnspecified {
			return singleErrorSequence(fmt.Errorf("unmapped chat models require provider-default reasoning"))
		}
		params.MaxTokens = params.MaxCompletionTokens
		params.MaxCompletionTokens = param.Opt[int64]{}
		params.Store = param.Opt[bool]{}
	}
	var options []option.RequestOption
	if m.vercel != nil {
		values, err := gateway.Options(*m.vercel, m.reasoning.Family, resolved.ThinkingLevel, false, adkmodels.OpenAIReasoningConfig{})
		if err != nil {
			return singleErrorSequence(err)
		}
		gateway.ForcedTools(values, req.Config)
		options = append(options, option.WithJSONSet("providerOptions", values))
	} else if m.reasoning.Family != family.Compatible {
		mapped, err := family.Map(family.OpenAI, resolved.ThinkingLevel)
		if err != nil {
			return singleErrorSequence(err)
		}
		params.ReasoningEffort = shared.ReasoningEffort(mapped.Effort)
		if err := validateChatToolReasoning(m.requestModel, mapped.Effort, len(params.Tools) > 0 && params.ToolChoice.OfAuto.Value != "none"); err != nil {
			return singleErrorSequence(err)
		}
	}
	if m.caching.Key != "" {
		params.PromptCacheKey = param.NewOpt(m.caching.Key)
	}
	return toolschema.RestoreOmissions(func(yield func(*model.LLMResponse, error) bool) {
		if stream {
			m.stream(ctx, params, options, yield)
			return
		}
		reply, err := m.client.Chat.Completions.New(ctx, params, options...)
		if err != nil {
			yield(nil, fmt.Errorf("openai chat: call failed: %w", err))
			return
		}
		response, err := converters.ChatToLLMResponse(reply)
		if err == nil {
			m.attachMetadata(response, reply.ID, reply.RawJSON())
		}
		yield(response, err)
	}, prepared)
}

var chatRestrictedModel = regexp.MustCompile(`^gpt-6-(luna|sol|astra)(?:-\d{4}-\d{2}-\d{2})?$`)

func validateChatToolReasoning(name, effort string, toolsEnabled bool) error {
	if !toolsEnabled {
		return nil
	}
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "openai/")
	match := chatRestrictedModel.FindStringSubmatch(name)
	if match == nil {
		return nil
	}
	if match[1] == "astra" {
		return fmt.Errorf("%s requires Responses for tool calls", name)
	}
	if effort != "none" {
		return fmt.Errorf("%s requires reasoning_effort=none for Chat Completions tool calls; use Responses to retain reasoning", name)
	}
	return nil
}

func (m *chatModel) stream(ctx context.Context, params sdk.ChatCompletionNewParams, options []option.RequestOption, yield func(*model.LLMResponse, error) bool) {
	params.StreamOptions.IncludeUsage = param.NewOpt(true)
	stream := m.client.Chat.Completions.NewStreaming(ctx, params, options...)
	defer func() { _ = stream.Close() }()
	var acc sdk.ChatCompletionAccumulator
	var gatewayMetadata adkmodels.Metadata
	var hasMetadata bool
	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			yield(nil, fmt.Errorf("openai chat: invalid stream chunk"))
			return
		}
		if parsed, ok := metadata.Parse(chunk.RawJSON()); ok {
			gatewayMetadata = parsed
			hasMetadata = true
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				yield(nil, fmt.Errorf("openai chat: multiple choices are unsupported"))
				return
			}
			text := choice.Delta.Content
			if choice.Delta.Refusal != "" {
				text += choice.Delta.Refusal
			}
			if text != "" && !yield(&model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: text}}}, Partial: true}, nil) {
				return
			}
		}
	}
	if err := stream.Err(); err != nil {
		yield(nil, fmt.Errorf("openai chat: stream failed: %w", err))
		return
	}
	response, err := converters.ChatToLLMResponse(&acc.ChatCompletion)
	if err != nil {
		yield(nil, err)
		return
	}
	m.attachMetadata(response, acc.ID, "")
	if m.vercel != nil && hasMetadata {
		metadata.AttachGateway(response, gatewayMetadata)
	}
	yield(response, nil)
}

func (m *chatModel) attachMetadata(resp *model.LLMResponse, id, raw string) {
	resp.CustomMetadata = make(map[string]any)
	metadata.Attach(resp, adkmodels.Metadata{ResponseID: id})
	if m.vercel != nil {
		if parsed, ok := metadata.Parse(raw); ok {
			metadata.AttachGateway(resp, parsed)
		}
	}
}
