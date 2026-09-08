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

// Package converters provides conversion functions between genai types and
// OpenAI Responses API types.
package converters

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

type requestBuildOptions struct {
	defaultMaxTokens int
}

// LLMRequestToResponseParams converts an ADK request to OpenAI Responses API
// parameters. Route-level reasoning, caching, and gateway policy are applied by
// the root adapter after this protocol conversion.
func LLMRequestToResponseParams(modelName shared.ResponsesModel, req *model.LLMRequest, defaultMaxTokens int) (responses.ResponseNewParams, error) {
	return buildOpenAIParamsWithOptions(modelName, req, requestBuildOptions{
		defaultMaxTokens: defaultMaxTokens,
	})
}

// ContentsToResponseInput converts ADK conversation content to Responses API
// input items, including function results and replayable reasoning state.
func ContentsToResponseInput(contents []*genai.Content) (responses.ResponseInputParam, error) {
	return convertContents(contents)
}

// buildOpenAIParams is kept small for converter tests and package-local use.
func buildOpenAIParams(modelName string, req *model.LLMRequest) (responses.ResponseNewParams, error) {
	requestModel := shared.ResponsesModel(modelName)
	if req != nil && req.Model != "" {
		requestModel = shared.ResponsesModel(req.Model)
	}
	return buildOpenAIParamsWithOptions(requestModel, req, requestBuildOptions{})
}

func buildOpenAIParamsWithOptions(modelName shared.ResponsesModel, req *model.LLMRequest, opts requestBuildOptions) (responses.ResponseNewParams, error) {
	if req == nil {
		return responses.ResponseNewParams{}, ErrRequestNil
	}
	params := responses.ResponseNewParams{Model: modelName}
	if opts.defaultMaxTokens > 0 {
		params.MaxOutputTokens = param.NewOpt(int64(opts.defaultMaxTokens))
	}

	input := responses.ResponseInputParam{}
	if req.Config != nil && req.Config.SystemInstruction != nil {
		instruction, err := flattenContentText(req.Config.SystemInstruction)
		if err != nil {
			return responses.ResponseNewParams{}, fmt.Errorf("openai: system instruction: %w", err)
		}
		if instruction != "" {
			input = append(input, responses.ResponseInputItemUnionParam{OfMessage: newInputMessage(
				responses.EasyInputMessageRoleDeveloper,
				responses.ResponseInputMessageContentListParam{inputText(instruction)},
			)})
		}
	}
	contents, err := convertContents(req.Contents)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	input = append(input, contents...)
	if len(input) == 0 {
		return responses.ResponseNewParams{}, ErrNoContents
	}
	params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}

	if err := applyGenerationConfig(&params, req.Config); err != nil {
		return responses.ResponseNewParams{}, err
	}
	tools, err := convertTools(req.Config)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	params.Tools = tools
	if cfg := req.Config; cfg != nil && cfg.ToolConfig != nil {
		choice, err := convertToolChoice(cfg.ToolConfig)
		if err != nil {
			return responses.ResponseNewParams{}, err
		}
		if choice != nil {
			params.ToolChoice = *choice
		}
	}
	return params, nil
}

func convertContents(contents []*genai.Content) (responses.ResponseInputParam, error) {
	var items responses.ResponseInputParam
	var tracker callTracker
	for _, content := range contents {
		if content == nil || len(content.Parts) == 0 {
			continue
		}
		role, err := normalizeRole(genai.Role(content.Role))
		if err != nil {
			return nil, err
		}
		var messageContent responses.ResponseInputMessageContentListParam
		var assistantText []string
		flushMessage := func() {
			if role == responses.EasyInputMessageRoleAssistant {
				if message := newOutputMessage(assistantText); message != nil {
					items = append(items, responses.ResponseInputItemUnionParam{OfOutputMessage: message})
				}
				assistantText = nil
				return
			}
			if message := newInputMessage(role, messageContent); message != nil {
				items = append(items, responses.ResponseInputItemUnionParam{OfMessage: message})
			}
			messageContent = nil
		}

		for _, part := range content.Parts {
			switch {
			case part == nil:
				continue
			case part.Thought && len(part.ThoughtSignature) > 0:
				flushMessage()
				reasoning, recognized, err := decodeReasoningSignature(part.ThoughtSignature)
				if err != nil {
					return nil, err
				}
				if !recognized {
					return nil, fmt.Errorf("openai: unsupported thought signature")
				}
				items = append(items, responses.ResponseInputItemUnionParam{OfReasoning: reasoning})
			case part.Thought:
				// Unsigned thought text is display-only and must not become a user
				// or assistant message when replaying history.
				continue
			case part.Text != "":
				if role == responses.EasyInputMessageRoleAssistant {
					assistantText = append(assistantText, part.Text)
				} else {
					messageContent = append(messageContent, inputText(part.Text))
				}
			case part.InlineData != nil:
				if role == responses.EasyInputMessageRoleAssistant {
					return nil, fmt.Errorf("openai: assistant media history is not supported")
				}
				media, err := inputMediaFromBytes(part.InlineData.Data, part.InlineData.MIMEType, part.InlineData.DisplayName)
				if err != nil {
					return nil, err
				}
				messageContent = append(messageContent, media)
			case part.FileData != nil:
				if role == responses.EasyInputMessageRoleAssistant {
					return nil, fmt.Errorf("openai: assistant media history is not supported")
				}
				media, err := inputMediaFromURI(part.FileData.FileURI, part.FileData.MIMEType, part.FileData.DisplayName)
				if err != nil {
					return nil, err
				}
				messageContent = append(messageContent, media)
			case part.FunctionCall != nil:
				flushMessage()
				call, err := tracker.newFunctionCall(part.FunctionCall)
				if err != nil {
					return nil, err
				}
				items = append(items, responses.ResponseInputItemUnionParam{OfFunctionCall: call})
			case part.FunctionResponse != nil:
				flushMessage()
				output, err := tracker.newFunctionResponse(part.FunctionResponse)
				if err != nil {
					return nil, err
				}
				items = append(items, responses.ResponseInputItemUnionParam{OfFunctionCallOutput: output})
			default:
				return nil, fmt.Errorf("openai: unsupported content part")
			}
		}
		flushMessage()
	}
	return items, nil
}

func inputText(text string) responses.ResponseInputContentUnionParam {
	return responses.ResponseInputContentUnionParam{OfInputText: &responses.ResponseInputTextParam{
		Text: text,
		Type: constant.InputText("input_text"),
	}}
}

func newInputMessage(role responses.EasyInputMessageRole, content responses.ResponseInputMessageContentListParam) *responses.EasyInputMessageParam {
	if len(content) == 0 {
		return nil
	}
	return &responses.EasyInputMessageParam{
		Role: role,
		Type: responses.EasyInputMessageTypeMessage,
		Content: responses.EasyInputMessageContentUnionParam{
			OfInputItemContentList: content,
		},
	}
}

func newOutputMessage(texts []string) *responses.ResponseOutputMessageParam {
	var content []responses.ResponseOutputMessageContentUnionParam
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		content = append(content, responses.ResponseOutputMessageContentUnionParam{
			OfOutputText: &responses.ResponseOutputTextParam{Text: text, Type: constant.OutputText("output_text")},
		})
	}
	if len(content) == 0 {
		return nil
	}
	return &responses.ResponseOutputMessageParam{Content: content, Status: responses.ResponseOutputMessageStatusCompleted}
}

func normalizeRole(role genai.Role) (responses.EasyInputMessageRole, error) {
	switch role {
	case "", genai.RoleUser:
		return responses.EasyInputMessageRoleUser, nil
	case genai.RoleModel:
		return responses.EasyInputMessageRoleAssistant, nil
	case "system":
		return responses.EasyInputMessageRoleSystem, nil
	case "developer":
		return responses.EasyInputMessageRoleDeveloper, nil
	default:
		return "", fmt.Errorf("openai: unsupported role %q", role)
	}
}

func inputMediaFromBytes(data []byte, mimeType, displayName string) (responses.ResponseInputContentUnionParam, error) {
	if len(data) == 0 {
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("openai: inline media is empty")
	}
	if mimeType == "" {
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("openai: inline media MIME type is required")
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{
			Detail:   responses.ResponseInputImageDetailAuto,
			ImageURL: param.NewOpt(dataURL),
			Type:     constant.InputImage("input_image"),
		}}, nil
	}
	return responses.ResponseInputContentUnionParam{OfInputFile: &responses.ResponseInputFileParam{
		FileData: param.NewOpt(dataURL),
		Filename: param.NewOpt(defaultFilename(displayName, mimeType)),
		Type:     constant.InputFile("input_file"),
	}}, nil
}

func inputMediaFromURI(uri, mimeType, displayName string) (responses.ResponseInputContentUnionParam, error) {
	if uri == "" {
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("openai: file URI is required")
	}
	if strings.HasPrefix(uri, "file-") {
		if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			return responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{
				Detail: responses.ResponseInputImageDetailAuto,
				FileID: param.NewOpt(uri),
				Type:   constant.InputImage("input_image"),
			}}, nil
		}
		return responses.ResponseInputContentUnionParam{OfInputFile: &responses.ResponseInputFileParam{
			FileID: param.NewOpt(uri),
			Type:   constant.InputFile("input_file"),
		}}, nil
	}
	parsed, err := url.Parse(uri)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "data") {
		return responses.ResponseInputContentUnionParam{}, fmt.Errorf("openai: file URI must be an OpenAI file ID or an http, https, or data URL")
	}
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{
			Detail:   responses.ResponseInputImageDetailAuto,
			ImageURL: param.NewOpt(uri),
			Type:     constant.InputImage("input_image"),
		}}, nil
	}
	file := &responses.ResponseInputFileParam{
		FileURL: param.NewOpt(uri),
		Type:    constant.InputFile("input_file"),
	}
	if displayName != "" {
		file.Filename = param.NewOpt(displayName)
	}
	return responses.ResponseInputContentUnionParam{OfInputFile: file}, nil
}

func defaultFilename(displayName, mimeType string) string {
	if displayName != "" {
		return displayName
	}
	extensions, _ := mime.ExtensionsByType(mimeType)
	if len(extensions) > 0 {
		return "attachment" + extensions[0]
	}
	return "attachment"
}

type callTracker struct {
	nextID  int
	pending []string
}

func (t *callTracker) newFunctionCall(fc *genai.FunctionCall) (*responses.ResponseFunctionToolCallParam, error) {
	if fc.Name == "" {
		return nil, ErrFunctionCallMissingName
	}
	callID := fc.ID
	if callID == "" {
		callID = fmt.Sprintf("adk-openai-call-%d", t.nextID)
		t.nextID++
	}
	t.pending = append(t.pending, callID)
	args := fc.Args
	if args == nil {
		args = map[string]any{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal function args: %w", err)
	}
	return &responses.ResponseFunctionToolCallParam{
		Name: fc.Name, CallID: callID, Arguments: string(raw), Type: constant.FunctionCall("function_call"),
	}, nil
}

func (t *callTracker) newFunctionResponse(fr *genai.FunctionResponse) (*responses.ResponseInputItemFunctionCallOutputParam, error) {
	callID := fr.ID
	if callID == "" {
		if len(t.pending) == 0 {
			return nil, fmt.Errorf("openai: response for %q missing call id", fr.Name)
		}
		callID = t.pending[0]
		t.pending = t.pending[1:]
	} else {
		found := false
		for i, pending := range t.pending {
			if pending == callID {
				t.pending = append(t.pending[:i], t.pending[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("openai: received function response for unknown or already completed call id %q", callID)
		}
	}
	raw, err := json.Marshal(fr.Response)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal function response: %w", err)
	}
	output := responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: param.NewOpt(string(raw))}
	if len(fr.Parts) > 0 {
		parts := responses.ResponseFunctionCallOutputItemListParam{
			{OfInputText: &responses.ResponseInputTextContentParam{Text: string(raw), Type: constant.InputText("input_text")}},
		}
		for _, part := range fr.Parts {
			media, err := functionOutputMedia(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, media)
		}
		output = responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfResponseFunctionCallOutputItemArray: parts}
	}
	return &responses.ResponseInputItemFunctionCallOutputParam{
		CallID: callID, Output: output, Type: constant.FunctionCallOutput("function_call_output"),
	}, nil
}

func functionOutputMedia(part *genai.FunctionResponsePart) (responses.ResponseFunctionCallOutputItemUnionParam, error) {
	if part == nil {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, fmt.Errorf("openai: nil function response media part")
	}
	if part.InlineData != nil {
		content, err := inputMediaFromBytes(part.InlineData.Data, part.InlineData.MIMEType, part.InlineData.DisplayName)
		if err != nil {
			return responses.ResponseFunctionCallOutputItemUnionParam{}, err
		}
		return functionOutputUnion(content), nil
	}
	if part.FileData != nil {
		content, err := inputMediaFromURI(part.FileData.FileURI, part.FileData.MIMEType, part.FileData.DisplayName)
		if err != nil {
			return responses.ResponseFunctionCallOutputItemUnionParam{}, err
		}
		return functionOutputUnion(content), nil
	}
	return responses.ResponseFunctionCallOutputItemUnionParam{}, fmt.Errorf("openai: empty function response media part")
}

func functionOutputUnion(content responses.ResponseInputContentUnionParam) responses.ResponseFunctionCallOutputItemUnionParam {
	switch {
	case content.OfInputImage != nil:
		return responses.ResponseFunctionCallOutputItemUnionParam{OfInputImage: &responses.ResponseInputImageContentParam{
			FileID: content.OfInputImage.FileID, ImageURL: content.OfInputImage.ImageURL,
			Detail: responses.ResponseInputImageContentDetail(content.OfInputImage.Detail), Type: constant.InputImage("input_image"),
		}}
	case content.OfInputFile != nil:
		return responses.ResponseFunctionCallOutputItemUnionParam{OfInputFile: &responses.ResponseInputFileContentParam{
			FileID: content.OfInputFile.FileID, FileData: content.OfInputFile.FileData,
			FileURL: content.OfInputFile.FileURL, Filename: content.OfInputFile.Filename,
			Detail: responses.ResponseInputFileContentDetail(content.OfInputFile.Detail), Type: constant.InputFile("input_file"),
		}}
	default:
		return responses.ResponseFunctionCallOutputItemUnionParam{}
	}
}

func applyGenerationConfig(params *responses.ResponseNewParams, cfg *genai.GenerateContentConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.Temperature != nil {
		params.Temperature = param.NewOpt(float64(*cfg.Temperature))
	}
	if cfg.TopP != nil {
		params.TopP = param.NewOpt(float64(*cfg.TopP))
	}
	if cfg.TopK != nil {
		return ErrTopKNotSupported
	}
	if cfg.MaxOutputTokens > 0 {
		params.MaxOutputTokens = param.NewOpt(int64(cfg.MaxOutputTokens))
	}
	if len(cfg.StopSequences) > 0 {
		return ErrStopSequencesNotSupported
	}
	if cfg.CandidateCount > 1 {
		return ErrMultipleCandidatesNotSupported
	}
	if cfg.FrequencyPenalty != nil || cfg.PresencePenalty != nil {
		return ErrPenaltiesNotSupported
	}
	if cfg.ResponseLogprobs {
		if cfg.Logprobs != nil {
			params.TopLogprobs = param.NewOpt(int64(*cfg.Logprobs))
		} else {
			params.TopLogprobs = param.NewOpt(int64(1))
		}
		params.Include = appendUniqueInclude(params.Include, responses.ResponseIncludableMessageOutputTextLogprobs)
	}
	if cfg.ResponseMIMEType != "" && cfg.ResponseMIMEType != "text/plain" && cfg.ResponseMIMEType != "application/json" {
		return fmt.Errorf("%w: %s", ErrUnsupportedMIMEType, cfg.ResponseMIMEType)
	}
	if cfg.ResponseMIMEType == "application/json" || cfg.ResponseSchema != nil || cfg.ResponseJsonSchema != nil {
		if cfg.ResponseSchema == nil && cfg.ResponseJsonSchema == nil {
			object := shared.NewResponseFormatJSONObjectParam()
			params.Text = responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONObject: &object}}
		} else {
			format, err := newJSONSchemaFormat(cfg)
			if err != nil {
				return err
			}
			params.Text = responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: format}}
		}
	}
	if cfg.Labels != nil {
		return ErrLabelsNotSupported
	}
	if cfg.SafetySettings != nil {
		return ErrSafetySettingsNotSupported
	}
	return nil
}

func appendUniqueInclude(includes []responses.ResponseIncludable, value responses.ResponseIncludable) []responses.ResponseIncludable {
	for _, include := range includes {
		if include == value {
			return includes
		}
	}
	return append(includes, value)
}

func flattenContentText(content *genai.Content) (string, error) {
	if content == nil {
		return "", nil
	}
	var builder strings.Builder
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.Text == "" {
			return "", fmt.Errorf("non-text system instruction part")
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}

func newJSONSchemaFormat(cfg *genai.GenerateContentConfig) (*responses.ResponseFormatTextJSONSchemaConfigParam, error) {
	var schema map[string]any
	var err error
	switch {
	case cfg.ResponseJsonSchema != nil:
		schema, err = normalizeSchema(cfg.ResponseJsonSchema)
	case cfg.ResponseSchema != nil:
		schema, err = schemaToMap(cfg.ResponseSchema)
	default:
		return nil, fmt.Errorf("openai: json schema requested without schema")
	}
	if err != nil {
		return nil, err
	}
	enforceStrictOpenAISchema(schema)
	name := "adk_response"
	if cfg.ResponseSchema != nil && cfg.ResponseSchema.Title != "" {
		name = cfg.ResponseSchema.Title
	}
	return &responses.ResponseFormatTextJSONSchemaConfigParam{
		Name: name, Schema: schema, Strict: param.NewOpt(true), Type: constant.JSONSchema("json_schema"),
	}, nil
}

func normalizeSchema(schema any) (map[string]any, error) {
	switch value := schema.(type) {
	case map[string]any:
		return value, nil
	case nil:
		return nil, ErrEmptyJSONSchema
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("openai: marshal json schema: %w", err)
		}
		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("openai: unmarshal json schema: %w", err)
		}
		return result, nil
	}
}

func enforceStrictOpenAISchema(value any) {
	schema, ok := value.(map[string]any)
	if !ok {
		return
	}
	if _, hasRef := schema["$ref"]; hasRef {
		for key := range schema {
			if key != "$ref" {
				delete(schema, key)
			}
		}
		return
	}
	typeValue, hasType := schema["type"]
	properties, hasProperties := schema["properties"]
	if hasType && typeValue == "object" && hasProperties {
		schema["additionalProperties"] = false
		if propertyMap, ok := properties.(map[string]any); ok {
			required := make([]string, 0, len(propertyMap))
			for key := range propertyMap {
				required = append(required, key)
			}
			sort.Strings(required)
			schema["required"] = required
		}
	}
	if definitions, ok := schema["$defs"].(map[string]any); ok {
		for _, definition := range definitions {
			enforceStrictOpenAISchema(definition)
		}
	}
	if propertyMap, ok := properties.(map[string]any); ok {
		for _, property := range propertyMap {
			enforceStrictOpenAISchema(property)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if values, ok := schema[key].([]any); ok {
			for _, child := range values {
				enforceStrictOpenAISchema(child)
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		enforceStrictOpenAISchema(items)
	}
}
