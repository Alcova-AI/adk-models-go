// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/Alcova-AI/adk-models-go/internal/family"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

var reasoningSignaturePrefix = []byte("vercel.ai-sdk.v4.reasoning:")

func buildCallOptions(req *model.LLMRequest, defaultMaxTokens int32) (protocol.CallOptions, error) {
	if req == nil {
		return protocol.CallOptions{}, ErrRequestNil
	}
	result := protocol.CallOptions{}
	if defaultMaxTokens > 0 {
		value := defaultMaxTokens
		result.MaxOutputTokens = &value
	}
	if req.Config != nil && req.Config.SystemInstruction != nil {
		text, err := flattenText(req.Config.SystemInstruction)
		if err != nil {
			return protocol.CallOptions{}, fmt.Errorf("vercel: system instruction: %w", err)
		}
		if text != "" {
			result.Prompt = append(result.Prompt, protocol.Message{Role: "system", Content: text})
		}
	}
	prompt, err := convertContents(req.Contents)
	if err != nil {
		return protocol.CallOptions{}, err
	}
	result.Prompt = append(result.Prompt, prompt...)
	if len(result.Prompt) == 0 {
		return protocol.CallOptions{}, ErrNoContents
	}
	if err := applyGenerationConfig(&result, req.Config); err != nil {
		return protocol.CallOptions{}, err
	}
	return result, nil
}

func convertContents(contents []*genai.Content) ([]protocol.Message, error) {
	var messages []protocol.Message
	tracker := callTracker{}
	for _, content := range contents {
		if content == nil || len(content.Parts) == 0 {
			continue
		}
		role, err := normaliseRole(content.Role)
		if err != nil {
			return nil, err
		}
		var parts []protocol.Part
		flush := func(messageRole string) {
			if len(parts) == 0 {
				return
			}
			messages = append(messages, protocol.Message{Role: messageRole, Content: parts})
			parts = nil
		}
		for _, part := range content.Parts {
			switch {
			case part == nil:
				continue
			case part.Thought && len(part.ThoughtSignature) > 0:
				reasoning, err := decodeReasoningPart(part.ThoughtSignature)
				if err != nil {
					return nil, err
				}
				parts = append(parts, reasoning)
			case part.Thought:
				continue
			case part.Text != "":
				parts = append(parts, protocol.Part{Type: "text", Text: part.Text})
			case part.InlineData != nil:
				file, err := inlineFile(part.InlineData)
				if err != nil {
					return nil, err
				}
				parts = append(parts, file)
			case part.FileData != nil:
				file, err := remoteFile(part.FileData)
				if err != nil {
					return nil, err
				}
				parts = append(parts, file)
			case part.FunctionCall != nil:
				call, err := tracker.functionCall(part.FunctionCall)
				if err != nil {
					return nil, err
				}
				parts = append(parts, call)
			case part.FunctionResponse != nil:
				flush(role)
				output, err := tracker.functionResponse(part.FunctionResponse)
				if err != nil {
					return nil, err
				}
				parts = append(parts, output)
				flush("tool")
			default:
				return nil, fmt.Errorf("vercel: unsupported content part")
			}
		}
		flush(role)
	}
	return messages, nil
}

func normaliseRole(role string) (string, error) {
	switch role {
	case "", string(genai.RoleUser):
		return "user", nil
	case string(genai.RoleModel):
		return "assistant", nil
	case "system", "developer":
		return "system", nil
	default:
		return "", fmt.Errorf("vercel: unsupported role %q", role)
	}
}

func inlineFile(blob *genai.Blob) (protocol.Part, error) {
	if len(blob.Data) == 0 {
		return protocol.Part{}, fmt.Errorf("vercel: inline media is empty")
	}
	if blob.MIMEType == "" {
		return protocol.Part{}, fmt.Errorf("vercel: inline media MIME type is required")
	}
	return protocol.Part{
		Type:      "file",
		Filename:  blob.DisplayName,
		MediaType: blob.MIMEType,
		Data:      &protocol.FileData{Type: "data", Data: base64.StdEncoding.EncodeToString(blob.Data)},
	}, nil
}

func remoteFile(file *genai.FileData) (protocol.Part, error) {
	if file.FileURI == "" {
		return protocol.Part{}, fmt.Errorf("vercel: file URI is required")
	}
	parsed, err := url.Parse(file.FileURI)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "data") {
		return protocol.Part{}, fmt.Errorf("vercel: file URI must be an http, https, or data URL")
	}
	return protocol.Part{
		Type:      "file",
		Filename:  file.DisplayName,
		MediaType: file.MIMEType,
		Data:      &protocol.FileData{Type: "url", URL: file.FileURI},
	}, nil
}

type callTracker struct {
	nextID  int
	pending []string
}

func (t *callTracker) functionCall(call *genai.FunctionCall) (protocol.Part, error) {
	if call.Name == "" {
		return protocol.Part{}, fmt.Errorf("vercel: function call missing name")
	}
	id := call.ID
	if id == "" {
		id = fmt.Sprintf("adk-vercel-call-%d", t.nextID)
		t.nextID++
	}
	t.pending = append(t.pending, id)
	input := call.Args
	if input == nil {
		input = map[string]any{}
	}
	return protocol.Part{Type: "tool-call", ToolCallID: id, ToolName: call.Name, Input: input}, nil
}

func (t *callTracker) functionResponse(response *genai.FunctionResponse) (protocol.Part, error) {
	id := response.ID
	if id == "" {
		if len(t.pending) == 0 {
			return protocol.Part{}, fmt.Errorf("vercel: response for %q missing call id", response.Name)
		}
		id = t.pending[0]
		t.pending = t.pending[1:]
	} else if !t.consume(id) {
		return protocol.Part{}, fmt.Errorf("vercel: response for unknown or completed call id %q", id)
	}
	output := protocol.ToolResultOutput{Type: "json", Value: response.Response}
	if len(response.Parts) > 0 {
		content := []any{map[string]any{"type": "text", "text": mustJSON(response.Response)}}
		for _, part := range response.Parts {
			if part == nil {
				return protocol.Part{}, fmt.Errorf("vercel: nil function response media part")
			}
			var file protocol.Part
			var err error
			if part.InlineData != nil {
				file, err = inlineFile(&genai.Blob{Data: part.InlineData.Data, MIMEType: part.InlineData.MIMEType, DisplayName: part.InlineData.DisplayName})
			} else if part.FileData != nil {
				file, err = remoteFile(&genai.FileData{FileURI: part.FileData.FileURI, MIMEType: part.FileData.MIMEType, DisplayName: part.FileData.DisplayName})
			} else {
				err = fmt.Errorf("vercel: empty function response media part")
			}
			if err != nil {
				return protocol.Part{}, err
			}
			content = append(content, file)
		}
		output = protocol.ToolResultOutput{Type: "content", Value: content}
	}
	return protocol.Part{Type: "tool-result", ToolCallID: id, ToolName: response.Name, Output: &output}, nil
}

func (t *callTracker) consume(id string) bool {
	for i, pending := range t.pending {
		if pending == id {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			return true
		}
	}
	return false
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func applyGenerationConfig(result *protocol.CallOptions, cfg *genai.GenerateContentConfig) error {
	if cfg == nil {
		return nil
	}
	result.Seed = cfg.Seed
	result.Temperature = cfg.Temperature
	result.TopP = cfg.TopP
	result.TopK = cfg.TopK
	result.FrequencyPenalty = cfg.FrequencyPenalty
	result.PresencePenalty = cfg.PresencePenalty
	if cfg.MaxOutputTokens > 0 {
		result.MaxOutputTokens = &cfg.MaxOutputTokens
	}
	result.StopSequences = append([]string(nil), cfg.StopSequences...)
	if cfg.CandidateCount > 1 {
		return fmt.Errorf("vercel: multiple candidates per request are not supported")
	}
	if cfg.Labels != nil {
		return fmt.Errorf("vercel: request labels are not supported")
	}
	if cfg.SafetySettings != nil {
		return fmt.Errorf("vercel: Gemini safety settings are not supported")
	}
	if cfg.ResponseMIMEType != "" && cfg.ResponseMIMEType != "text/plain" && cfg.ResponseMIMEType != "application/json" {
		return fmt.Errorf("vercel: unsupported response MIME type %q", cfg.ResponseMIMEType)
	}
	if cfg.ResponseMIMEType == "application/json" || cfg.ResponseSchema != nil || cfg.ResponseJsonSchema != nil {
		format := map[string]any{"type": "json"}
		if cfg.ResponseSchema != nil {
			schema, err := schemaToMap(cfg.ResponseSchema)
			if err != nil {
				return err
			}
			format["schema"] = schema
		} else if cfg.ResponseJsonSchema != nil {
			format["schema"] = cfg.ResponseJsonSchema
		}
		result.ResponseFormat = format
	}
	tools, err := convertTools(cfg.Tools)
	if err != nil {
		return err
	}
	result.Tools = tools
	applyToolConfig(result, cfg.ToolConfig)
	if cfg.ThinkingConfig != nil {
		if cfg.ThinkingConfig.ThinkingBudget != nil {
			return fmt.Errorf("vercel: ThinkingBudget is not supported; use ThinkingLevel")
		}
		level, err := thinkingLevel(cfg.ThinkingConfig.ThinkingLevel)
		if err != nil {
			return err
		}
		result.Reasoning = level
	}
	return nil
}

func convertTools(source []*genai.Tool) ([]protocol.FunctionTool, error) {
	var result []protocol.FunctionTool
	for i, tool := range source {
		if tool == nil {
			return nil, fmt.Errorf("vercel: tool %d is nil", i)
		}
		if tool.Retrieval != nil || tool.GoogleSearch != nil || tool.GoogleSearchRetrieval != nil || tool.GoogleMaps != nil || tool.EnterpriseWebSearch != nil || tool.URLContext != nil || tool.ComputerUse != nil || tool.CodeExecution != nil {
			return nil, fmt.Errorf("vercel: non-function tools are not supported (tool %d)", i)
		}
		for _, declaration := range tool.FunctionDeclarations {
			if declaration == nil || declaration.Name == "" {
				return nil, fmt.Errorf("vercel: function declaration missing name")
			}
			schema, err := schemaToMap(declaration.Parameters)
			if err != nil {
				return nil, err
			}
			if schema == nil {
				schema, err = normaliseJSONSchema(declaration.ParametersJsonSchema)
				if err != nil {
					return nil, err
				}
			}
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			result = append(result, protocol.FunctionTool{Type: "function", Name: declaration.Name, Description: declaration.Description, InputSchema: schema})
		}
	}
	return result, nil
}

func applyToolConfig(result *protocol.CallOptions, cfg *genai.ToolConfig) {
	if cfg == nil || cfg.FunctionCallingConfig == nil {
		return
	}
	callCfg := cfg.FunctionCallingConfig
	allowed := make(map[string]bool, len(callCfg.AllowedFunctionNames))
	for _, name := range callCfg.AllowedFunctionNames {
		allowed[name] = true
	}
	if len(allowed) > 0 {
		filtered := result.Tools[:0]
		for _, tool := range result.Tools {
			if allowed[tool.Name] {
				filtered = append(filtered, tool)
			}
		}
		result.Tools = filtered
	}
	switch callCfg.Mode {
	case genai.FunctionCallingConfigModeNone:
		result.ToolChoice = &protocol.ToolChoice{Type: "none"}
	case genai.FunctionCallingConfigModeAny:
		result.ToolChoice = &protocol.ToolChoice{Type: "required"}
	default:
		result.ToolChoice = &protocol.ToolChoice{Type: "auto"}
	}
}

func thinkingLevel(level genai.ThinkingLevel) (string, error) {
	if !family.ValidLevel(level) {
		return "", fmt.Errorf("vercel: unsupported thinking level %q", level)
	}
	if level == "" || level == genai.ThinkingLevelUnspecified {
		return "", nil
	}
	return strings.ToLower(string(level)), nil
}

func flattenText(content *genai.Content) (string, error) {
	var result strings.Builder
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.Text == "" {
			return "", fmt.Errorf("non-text part")
		}
		result.WriteString(part.Text)
	}
	return result.String(), nil
}

func schemaToMap(schema *genai.Schema) (map[string]any, error) {
	if schema == nil {
		return nil, nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("vercel: marshal schema: %w", err)
	}
	return normaliseJSONSchema(raw)
}

func normaliseJSONSchema(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if bytesValue, ok := value.([]byte); ok {
		raw = bytesValue
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("vercel: marshal JSON schema: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("vercel: decode JSON schema: %w", err)
	}
	lowercaseTypes(result)
	return result, nil
}

func lowercaseTypes(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if typeValue, ok := typed["type"].(string); ok {
			typed["type"] = strings.ToLower(typeValue)
		}
		for _, child := range typed {
			lowercaseTypes(child)
		}
	case []any:
		for _, child := range typed {
			lowercaseTypes(child)
		}
	}
}

func encodeReasoningPart(part protocol.OutputPart) ([]byte, error) {
	raw, err := json.Marshal(protocol.Part{Type: "reasoning", Text: part.Text, ProviderOptions: cloneMetadata(part.ProviderMetadata)})
	if err != nil {
		return nil, fmt.Errorf("vercel: encode reasoning state: %w", err)
	}
	return append(append([]byte(nil), reasoningSignaturePrefix...), raw...), nil
}

func decodeReasoningPart(signature []byte) (protocol.Part, error) {
	if !bytes.HasPrefix(signature, reasoningSignaturePrefix) {
		return protocol.Part{}, fmt.Errorf("vercel: unsupported thought signature")
	}
	var result protocol.Part
	if err := json.Unmarshal(signature[len(reasoningSignaturePrefix):], &result); err != nil {
		return protocol.Part{}, fmt.Errorf("vercel: decode reasoning state: %w", err)
	}
	return result, nil
}

func cloneMetadata(source map[string]any) protocol.ProviderOptions {
	if len(source) == 0 {
		return nil
	}
	result := make(protocol.ProviderOptions)
	for namespace, value := range source {
		if values, ok := value.(map[string]any); ok {
			result[namespace] = values
		}
	}
	return result
}
