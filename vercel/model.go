// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Alcova-AI/adk-models-go/internal/jsonschema"
	"io"
	"iter"
	"net/http"
	"strings"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/Alcova-AI/adk-models-go/internal/gateway"
	vercelopenai "github.com/Alcova-AI/adk-models-go/internal/vercelopenai"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

const defaultMaxTokens int32 = 16384

type gatewayModel struct {
	schemas          *toolschema.Processor
	apiKey           string
	canonicalModel   string
	requestModel     string
	baseURL          string
	httpClient       *http.Client
	headers          http.Header
	defaultMaxTokens int32
	config           adkmodels.ModelConfig
	family           family.Family
}

func NewModel(cfg Config) (model.LLM, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("AI Gateway API key is required")
	}
	if err := cfg.Model.Validate(); err != nil {
		return nil, err
	}
	cfg.Model.Vercel = gateway.CloneConfig(cfg.Model.Vercel)
	f, err := family.Detect(cfg.Model.CanonicalModel)
	if err != nil {
		return nil, err
	}
	if f == family.OpenAI {
		if err := cfg.Model.PromptCaching.OpenAI.Validate(); err != nil {
			return nil, err
		}
	}
	requestModel := cfg.Model.RequestModel
	if requestModel == "" {
		requestModel = cfg.Model.CanonicalModel
	}
	tokens := cfg.Model.DefaultMaxOutputTokens
	if tokens == 0 {
		tokens = defaultMaxTokens
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Transport: &retryTransport{base: http.DefaultTransport, sleep: retrySleep}}
	}
	return &gatewayModel{schemas: toolschema.New(cfg.Model.ToolSchemas, toolschema.Target{Provider: string(f), Route: "vercel-native"}), apiKey: cfg.APIKey, canonicalModel: cfg.Model.CanonicalModel, requestModel: requestModel,
		baseURL: normaliseBaseURL(cfg.BaseURL), httpClient: client, headers: cfg.Headers.Clone(), defaultMaxTokens: tokens,
		config: cfg.Model, family: f}, nil
}

func (m *gatewayModel) Name() string { return m.canonicalModel }

func (m *gatewayModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if req == nil {
		return singleError(ErrRequestNil)
	}
	prepared, err := m.schemas.Prepare(ctx, toolschema.Tools(req))
	if err != nil {
		return singleError(err)
	}
	options, err := buildCallOptions(req, m.defaultMaxTokens)
	if err != nil {
		return singleError(fmt.Errorf("failed to convert request: %w", err))
	}
	for i := range options.Tools {
		schema := prepared[options.Tools[i].Name]
		options.Tools[i].InputSchema = schema.Schema
		options.Tools[i].Strict = schema.Strict
	}
	var thinking *genai.ThinkingConfig
	if req.Config != nil {
		thinking = req.Config.ThinkingConfig
	}
	level, include, err := family.Resolve(m.config.Reasoning.DefaultLevel, thinking)
	if err != nil {
		return singleError(err)
	}
	cfg := adkmodels.VercelConfig{}
	if m.config.Vercel != nil {
		cfg = *m.config.Vercel
	}
	providerOptions, err := gateway.Options(cfg, m.family, level, include, m.config.Reasoning.OpenAI)
	if err != nil {
		return singleError(err)
	}
	gateway.ForcedTools(providerOptions, req.Config)
	options.ProviderOptions = providerOptions
	options.Reasoning = "" // The shared provider-specific mapping owns effort.
	if m.family == family.OpenAI {
		if options.ResponseFormat != nil {
			jsonschema.EnforceOpenAI(options.ResponseFormat["schema"])
		}
		cache := vercelopenai.Options{PromptCaching: m.config.PromptCaching.OpenAI}
		if err := cache.Apply(&options); err != nil {
			return singleError(err)
		}
	}
	if stream {
		return m.generateStream(ctx, options, includesThoughts(req))
	}
	return m.generate(ctx, options, includesThoughts(req))
}

func (m *gatewayModel) generate(ctx context.Context, options protocol.CallOptions, includeThoughts bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		response, err := m.post(ctx, options, false)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() { _ = response.Body.Close() }()
		if err := checkGatewayResponse(response); err != nil {
			yield(nil, err)
			return
		}
		var result protocol.GenerateResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			yield(nil, fmt.Errorf("vercel: decode response: %w", err))
			return
		}
		converted, err := convertGenerateResult(result, includeThoughts)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(converted, nil)
	}
}

func (m *gatewayModel) post(ctx context.Context, options protocol.CallOptions, stream bool) (*http.Response, error) {
	body, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("vercel: encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/language-model", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vercel: create request: %w", err)
	}
	for key, values := range m.headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	request.Header.Set("Authorization", "Bearer "+m.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", map[bool]string{true: "text/event-stream", false: "application/json"}[stream])
	request.Header.Set("ai-gateway-protocol-version", GatewayProtocolVersion)
	request.Header.Set("ai-gateway-auth-method", "api-key")
	request.Header.Set("ai-language-model-specification-version", LanguageModelSpecificationVersion)
	request.Header.Set("ai-language-model-id", m.requestModel)
	request.Header.Set("ai-language-model-streaming", fmt.Sprintf("%t", stream))
	request.Header.Set("User-Agent", "adk-vercel-go ai-sdk/gateway/"+PinnedGatewayPackageVersion)
	response, err := m.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("vercel: call failed: %w", err)
	}
	return response, nil
}

func checkGatewayResponse(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	message := strings.TrimSpace(string(raw))
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Error.Message != "" {
		message = payload.Error.Message
	}
	return &GatewayError{StatusCode: response.StatusCode, Message: message, Body: string(raw)}
}

func includesThoughts(req *model.LLMRequest) bool {
	return req.Config != nil && req.Config.ThinkingConfig != nil && req.Config.ThinkingConfig.IncludeThoughts
}

func singleError(err error) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, err) }
}
