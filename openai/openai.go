// Copyright 2025 Google LLC
// Modified by Alcova AI, 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adkopenai

import (
	"context"
	"fmt"
	"iter"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/Alcova-AI/adk-models-go/internal/gateway"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"
	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	"google.golang.org/adk/v2/model"
)

const defaultMaxTokens = 16384

type openAIModel struct {
	schemas          *toolschema.Processor
	client           openai.Client
	canonicalModel   shared.ResponsesModel
	requestModel     shared.ResponsesModel
	defaultMaxTokens int
	reasoning        reasoningConfig
	promptCaching    adkmodels.OpenAIPromptCachingConfig
	vercel           *adkmodels.VercelConfig
	family           family.Family
}

// NewModel returns an ADK model backed by a caller-owned OpenAI SDK client.
// The adapter does not discover credentials, select endpoints, or infer gateway
// capabilities from model names.
func NewModel(cfg Config) (model.LLM, error) {
	if len(cfg.Client.Options) == 0 {
		return nil, fmt.Errorf("client must be constructed with openai.NewClient")
	}
	if err := cfg.Model.Validate(); err != nil {
		return nil, err
	}
	cfg.Model.Vercel = gateway.CloneConfig(cfg.Model.Vercel)
	f, err := family.Detect(cfg.Model.CanonicalModel)
	if err != nil {
		return nil, err
	}
	if f != family.OpenAI && cfg.Model.Vercel == nil {
		return nil, fmt.Errorf("direct openai adapter requires its own model family; cross-family requests require Vercel")
	}
	cache := cfg.Model.PromptCaching.OpenAI
	if f == family.OpenAI {
		if err := cache.Validate(); err != nil {
			return nil, err
		}
	} else {
		cache = adkmodels.OpenAIPromptCachingConfig{}
	}
	requestModel := cfg.Model.RequestModel
	if requestModel == "" {
		requestModel = cfg.Model.CanonicalModel
	}
	tokens := int(cfg.Model.DefaultMaxOutputTokens)
	if tokens == 0 {
		tokens = defaultMaxTokens
	}
	route := "direct"
	if cfg.Model.Vercel != nil {
		route = "vercel-openai"
	}
	return &openAIModel{
		schemas: toolschema.New(cfg.Model.ToolSchemas, toolschema.Target{Provider: string(f), Route: route}),
		client:  cfg.Client, canonicalModel: shared.ResponsesModel(cfg.Model.CanonicalModel), requestModel: shared.ResponsesModel(requestModel),
		defaultMaxTokens: tokens, reasoning: reasoningConfig{DefaultLevel: cfg.Model.Reasoning.DefaultLevel, OpenAI: cfg.Model.Reasoning.OpenAI, Family: f},
		promptCaching: cache, vercel: cfg.Model.Vercel, family: f,
	}, nil
}

// Name returns the model name.
func (m *openAIModel) Name() string { return string(m.canonicalModel) }

func (m *openAIModel) wireModel() shared.ResponsesModel {
	if m.requestModel != "" {
		return m.requestModel
	}
	return m.canonicalModel
}

// GenerateContent converts an ADK request and calls the Responses API.
func (m *openAIModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if req == nil {
		return singleErrorSequence(ErrRequestNil)
	}
	prepared, err := m.schemas.Prepare(ctx, toolschema.Tools(req))
	if err != nil {
		return singleErrorSequence(err)
	}
	params, err := m.convertRequest(req)
	if err != nil {
		return singleErrorSequence(err)
	}
	for i := range params.Tools {
		if fn := params.Tools[i].OfFunction; fn != nil {
			schema := prepared[fn.Name]
			fn.Parameters = nil
			fn.SetExtraFields(map[string]any{"parameters": schema.JSONSchema})
			if schema.Strict != nil {
				fn.Strict = param.NewOpt(*schema.Strict)
			}
		}
	}
	requestOptions, err := m.requestOptions(req, params.MaxOutputTokens.Or(0))
	if err != nil {
		return singleErrorSequence(err)
	}
	if stream {
		return toolschema.RestoreOmissions(m.generateStream(ctx, params, requestOptions, requestIncludesThoughts(req)), prepared)
	}
	return toolschema.RestoreOmissions(m.generate(ctx, params, requestOptions, requestIncludesThoughts(req)), prepared)
}

func (m *openAIModel) convertRequest(req *model.LLMRequest) (responses.ResponseNewParams, error) {
	params, err := converters.LLMRequestToResponseParams(m.wireModel(), req, m.defaultMaxTokens)
	if err != nil {
		return responses.ResponseNewParams{}, fmt.Errorf("failed to convert request: %w", err)
	}
	if err := applyReasoning(&params, req.Config, m.reasoning); err != nil {
		return responses.ResponseNewParams{}, fmt.Errorf("failed to configure reasoning: %w", err)
	}
	if err := applyPromptCaching(&params, m.promptCaching); err != nil {
		return responses.ResponseNewParams{}, fmt.Errorf("failed to configure prompt caching: %w", err)
	}
	{
		params.Store = param.NewOpt(false)
		params.Include = appendUniqueInclude(params.Include, responses.ResponseIncludableReasoningEncryptedContent)
	}
	return params, nil
}

func (m *openAIModel) requestOptions(req *model.LLMRequest, maxOutputTokens int64) ([]option.RequestOption, error) {
	if m.vercel == nil {
		return nil, nil
	}
	var thinking *genai.ThinkingConfig
	if req != nil && req.Config != nil {
		thinking = req.Config.ThinkingConfig
	}
	resolved, err := m.reasoning.resolve(thinking)
	if err != nil {
		return nil, err
	}
	options, err := gateway.Options(*m.vercel, m.family, resolved.ThinkingLevel, resolved.IncludeThoughts, m.reasoning.OpenAI)
	if err != nil {
		return nil, err
	}
	if req != nil {
		gateway.ForcedTools(options, req.Config)
	}
	return []option.RequestOption{option.WithJSONSet("providerOptions", options)}, nil
}

func (m *openAIModel) generate(ctx context.Context, params responses.ResponseNewParams, requestOptions []option.RequestOption, includeThoughts bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.client.Responses.New(ctx, params, requestOptions...)
		if err != nil {
			yield(nil, fmt.Errorf("openai: call failed: %w", err))
			return
		}
		llmResp, err := converters.ResponseToLLMResponse(resp, includeThoughts)
		if err != nil {
			yield(nil, err)
			return
		}
		attachOpenAIResponseMetadata(llmResp, resp)
		m.attachVercelResponseMetadata(llmResp, resp.RawJSON())
		yield(llmResp, nil)
	}
}

func (m *openAIModel) generateStream(ctx context.Context, params responses.ResponseNewParams, requestOptions []option.RequestOption, includeThoughts bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		stream := m.client.Responses.NewStreaming(ctx, params, requestOptions...)
		defer func() { _ = stream.Close() }()

		translator := newStreamTranslator()
		var finalResponse *responses.Response
		var gatewayMetadata adkmodels.Metadata
		hasGatewayMetadata := false

		for stream.Next() {
			event := stream.Current()
			if metadata, ok := metadata.Parse(event.RawJSON()); ok {
				gatewayMetadata = metadata
				hasGatewayMetadata = true
			}
			switch event.Type {
			case responseCompleted:
				value := event.AsResponseCompleted().Response
				finalResponse = &value
			case responseIncomplete:
				value := event.AsResponseIncomplete().Response
				finalResponse = &value
			}

			if !includeThoughts && (event.Type == responseReasoningTextDelta || event.Type == responseReasoningSummaryTextDelta) {
				continue
			}

			genaiResp, err := translator.process(event)
			if err != nil {
				yield(nil, err)
				return
			}
			if genaiResp == nil {
				continue
			}
			partial := converters.GenerateContentResponseToLLMResponse(genaiResp)
			partial.Partial = true
			if !yield(partial, nil) {
				return
			}
		}
		if err := stream.Err(); err != nil {
			yield(nil, fmt.Errorf("openai: stream failed: %w", err))
			return
		}
		if finalResponse == nil {
			yield(nil, ErrMissingTerminalResponse)
			return
		}
		final, err := converters.ResponseToLLMResponse(finalResponse, includeThoughts)
		if err != nil {
			yield(nil, err)
			return
		}
		final.TurnComplete = true
		attachOpenAIResponseMetadata(final, finalResponse)
		if hasGatewayMetadata {
			m.attachParsedResponseMetadata(final, gatewayMetadata)
		} else {
			m.attachVercelResponseMetadata(final, finalResponse.RawJSON())
		}
		yield(final, nil)
	}
}

func requestIncludesThoughts(req *model.LLMRequest) bool {
	return req != nil && req.Config != nil && req.Config.ThinkingConfig != nil && req.Config.ThinkingConfig.IncludeThoughts
}

func (m *openAIModel) attachVercelResponseMetadata(resp *model.LLMResponse, rawJSON string) {
	if m.vercel == nil || resp == nil {
		return
	}
	parsed, ok := metadata.Parse(rawJSON)
	if !ok {
		return
	}
	m.attachParsedResponseMetadata(resp, parsed)
}

func (m *openAIModel) attachParsedResponseMetadata(resp *model.LLMResponse, parsed adkmodels.Metadata) {
	if m.vercel == nil || resp == nil {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = make(map[string]any)
	}
	metadata.AttachGateway(resp, parsed)
}

func singleErrorSequence(err error) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, err) }
}
