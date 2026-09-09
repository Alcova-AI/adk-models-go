// Copyright 2023 Vercel, Inc.
// Copyright 2026 Alcova AI
// Modified by Alcova AI.
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

// Package protocol contains the pinned Vercel AI SDK Language Model V4 wire
// types used by the adapter.
package protocol

import "encoding/json"

type ProviderOptions map[string]map[string]any

type CallOptions struct {
	Prompt           []Message       `json:"prompt"`
	MaxOutputTokens  *int32          `json:"maxOutputTokens,omitempty"`
	Temperature      *float32        `json:"temperature,omitempty"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	TopP             *float32        `json:"topP,omitempty"`
	TopK             *float32        `json:"topK,omitempty"`
	PresencePenalty  *float32        `json:"presencePenalty,omitempty"`
	FrequencyPenalty *float32        `json:"frequencyPenalty,omitempty"`
	ResponseFormat   map[string]any  `json:"responseFormat,omitempty"`
	Seed             *int32          `json:"seed,omitempty"`
	Tools            []FunctionTool  `json:"tools,omitempty"`
	ToolChoice       *ToolChoice     `json:"toolChoice,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`
	ProviderOptions  ProviderOptions `json:"providerOptions,omitempty"`
}

type Message struct {
	Role            string          `json:"role"`
	Content         any             `json:"content"`
	ProviderOptions ProviderOptions `json:"providerOptions,omitempty"`
}

type Part struct {
	Type             string            `json:"type"`
	Text             string            `json:"text,omitempty"`
	Filename         string            `json:"filename,omitempty"`
	Data             *FileData         `json:"data,omitempty"`
	MediaType        string            `json:"mediaType,omitempty"`
	ToolCallID       string            `json:"toolCallId,omitempty"`
	ToolName         string            `json:"toolName,omitempty"`
	Input            any               `json:"input,omitempty"`
	Output           *ToolResultOutput `json:"output,omitempty"`
	ProviderExecuted bool              `json:"providerExecuted,omitempty"`
	ProviderOptions  ProviderOptions   `json:"providerOptions,omitempty"`
}

// MarshalJSON preserves the required text field for text and reasoning parts,
// including hidden reasoning whose text is intentionally empty. Other part
// variants omit text so the discriminated union stays exact.
func (p Part) MarshalJSON() ([]byte, error) {
	if p.Type == "text" || p.Type == "reasoning" {
		return json.Marshal(struct {
			Type            string          `json:"type"`
			Text            string          `json:"text"`
			ProviderOptions ProviderOptions `json:"providerOptions,omitempty"`
		}{Type: p.Type, Text: p.Text, ProviderOptions: p.ProviderOptions})
	}
	type partAlias Part
	return json.Marshal(partAlias(p))
}

type FileData struct {
	Type      string            `json:"type"`
	Data      string            `json:"data,omitempty"`
	URL       string            `json:"url,omitempty"`
	Reference map[string]string `json:"reference,omitempty"`
	Text      string            `json:"text,omitempty"`
}

type ToolResultOutput struct {
	Type  string `json:"type"`
	Value any    `json:"value,omitempty"`
}

type FunctionTool struct {
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	InputSchema     map[string]any  `json:"inputSchema"`
	Strict          *bool           `json:"strict,omitempty"`
	ProviderOptions ProviderOptions `json:"providerOptions,omitempty"`
}

type ToolChoice struct {
	Type     string `json:"type"`
	ToolName string `json:"toolName,omitempty"`
}

type GenerateResult struct {
	Content          []OutputPart      `json:"content"`
	FinishReason     FinishReason      `json:"finishReason"`
	Usage            Usage             `json:"usage"`
	ProviderMetadata map[string]any    `json:"providerMetadata,omitempty"`
	Response         *ResponseMetadata `json:"response,omitempty"`
}

type OutputPart struct {
	Type             string         `json:"type"`
	Text             string         `json:"text,omitempty"`
	Delta            string         `json:"delta,omitempty"`
	ID               string         `json:"id,omitempty"`
	ToolCallID       string         `json:"toolCallId,omitempty"`
	ToolName         string         `json:"toolName,omitempty"`
	Input            string         `json:"input,omitempty"`
	Result           any            `json:"result,omitempty"`
	IsError          bool           `json:"isError,omitempty"`
	MediaType        string         `json:"mediaType,omitempty"`
	Data             *FileData      `json:"data,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

type FinishReason struct {
	Unified string `json:"unified"`
	Raw     string `json:"raw,omitempty"`
}

type Usage struct {
	InputTokens  InputTokens    `json:"inputTokens"`
	OutputTokens OutputTokens   `json:"outputTokens"`
	Raw          map[string]any `json:"raw,omitempty"`
}

type InputTokens struct {
	Total      *int64 `json:"total"`
	NoCache    *int64 `json:"noCache"`
	CacheRead  *int64 `json:"cacheRead"`
	CacheWrite *int64 `json:"cacheWrite"`
}

type OutputTokens struct {
	Total     *int64 `json:"total"`
	Text      *int64 `json:"text"`
	Reasoning *int64 `json:"reasoning"`
}

type ResponseMetadata struct {
	ID        string `json:"id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	ModelID   string `json:"modelId,omitempty"`
}

type StreamPart struct {
	Type             string         `json:"type"`
	ID               string         `json:"id,omitempty"`
	Delta            string         `json:"delta,omitempty"`
	ToolName         string         `json:"toolName,omitempty"`
	ToolCallID       string         `json:"toolCallId,omitempty"`
	Input            string         `json:"input,omitempty"`
	Result           any            `json:"result,omitempty"`
	IsError          bool           `json:"isError,omitempty"`
	MediaType        string         `json:"mediaType,omitempty"`
	Data             *FileData      `json:"data,omitempty"`
	FinishReason     FinishReason   `json:"finishReason,omitempty"`
	Usage            Usage          `json:"usage,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
	Error            any            `json:"error,omitempty"`
	ModelID          string         `json:"modelId,omitempty"`
}
