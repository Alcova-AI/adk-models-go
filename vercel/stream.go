// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strings"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/internal/metadata"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

func (m *gatewayModel) generateStream(ctx context.Context, options protocol.CallOptions, includeThoughts bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		response, err := m.post(ctx, options, true)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() { _ = response.Body.Close() }()
		if err := checkGatewayResponse(response); err != nil {
			yield(nil, err)
			return
		}
		state := newStreamState()
		err = readSSE(response.Body, func(data []byte) error {
			if string(data) == "[DONE]" {
				return nil
			}
			var part protocol.StreamPart
			if err := json.Unmarshal(data, &part); err != nil {
				return fmt.Errorf("vercel: decode stream part: %w", err)
			}
			converted, terminal, err := state.convert(part, includeThoughts)
			if err != nil {
				return err
			}
			if converted != nil && !yield(converted, nil) {
				return errConsumerStopped
			}
			if terminal {
				state.finished = true
			}
			return nil
		})
		if err == errConsumerStopped {
			return
		}
		if err != nil {
			yield(nil, err)
			return
		}
		if !state.finished {
			yield(nil, ErrMissingFinish)
		}
	}
}

var errConsumerStopped = fmt.Errorf("consumer stopped")

type streamState struct {
	content           []*genai.Part
	textParts         map[string]*genai.Part
	reasoning         map[string]*strings.Builder
	reasoningParts    map[string]*genai.Part
	reasoningMetadata map[string]map[string]any
	responseID        string
	modelID           string
	finished          bool
}

func newStreamState() streamState {
	state := streamState{}
	state.initialize()
	return state
}

func (s *streamState) initialize() {
	if s.textParts == nil {
		s.textParts = map[string]*genai.Part{}
	}
	if s.reasoning == nil {
		s.reasoning = map[string]*strings.Builder{}
	}
	if s.reasoningParts == nil {
		s.reasoningParts = map[string]*genai.Part{}
	}
	if s.reasoningMetadata == nil {
		s.reasoningMetadata = map[string]map[string]any{}
	}
}

func (s *streamState) convert(part protocol.StreamPart, includeThoughts bool) (*model.LLMResponse, bool, error) {
	s.initialize()
	switch part.Type {
	case "stream-start", "tool-input-start", "tool-input-delta", "tool-input-end", "raw", "source", "custom", "tool-approval-request":
		return nil, false, nil
	case "text-start":
		s.startText(part.ID)
		return nil, false, nil
	case "text-end":
		delete(s.textParts, part.ID)
		return nil, false, nil
	case "response-metadata":
		s.modelID = part.ModelID
		s.responseID = part.ID
		return nil, false, nil
	case "text-delta":
		if part.Delta == "" {
			return nil, false, nil
		}
		text := s.startText(part.ID)
		text.Text += part.Delta
		return partial(&genai.Part{Text: part.Delta}), false, nil
	case "reasoning-start":
		s.reasoning[part.ID] = &strings.Builder{}
		reasoningPart := &genai.Part{Thought: true}
		s.reasoningParts[part.ID] = reasoningPart
		s.content = append(s.content, reasoningPart)
		s.reasoningMetadata[part.ID] = part.ProviderMetadata
		return nil, false, nil
	case "reasoning-delta":
		builder := s.reasoning[part.ID]
		if builder == nil {
			builder = &strings.Builder{}
			s.reasoning[part.ID] = builder
			reasoningPart := &genai.Part{Thought: true}
			s.reasoningParts[part.ID] = reasoningPart
			s.content = append(s.content, reasoningPart)
		}
		builder.WriteString(part.Delta)
		if includeThoughts {
			s.reasoningParts[part.ID].Text += part.Delta
		}
		if includeThoughts && part.Delta != "" {
			return partial(&genai.Part{Text: part.Delta, Thought: true}), false, nil
		}
		return nil, false, nil
	case "reasoning-end":
		builder := s.reasoning[part.ID]
		metadata := mergeMetadata(s.reasoningMetadata[part.ID], part.ProviderMetadata)
		output := protocol.OutputPart{Type: "reasoning", ID: part.ID, ProviderMetadata: metadata}
		if builder != nil {
			output.Text = builder.String()
		}
		signature, err := encodeReasoningPart(output)
		delete(s.reasoning, part.ID)
		delete(s.reasoningMetadata, part.ID)
		if err != nil {
			return nil, false, err
		}
		reasoningPart := s.reasoningParts[part.ID]
		if reasoningPart == nil {
			reasoningPart = &genai.Part{Thought: true}
			s.content = append(s.content, reasoningPart)
		}
		reasoningPart.ThoughtSignature = signature
		if includeThoughts {
			reasoningPart.Text = output.Text
		}
		return partial(&genai.Part{Thought: true, ThoughtSignature: signature}), false, nil
	case "tool-call":
		converted, err := convertOutputPart(protocol.OutputPart{Type: part.Type, ToolCallID: part.ToolCallID, ToolName: part.ToolName, Input: part.Input, ProviderMetadata: part.ProviderMetadata}, includeThoughts)
		if err != nil {
			return nil, false, err
		}
		s.content = append(s.content, converted)
		return partial(converted), false, nil
	case "tool-result":
		converted, err := convertOutputPart(protocol.OutputPart{Type: part.Type, ToolCallID: part.ToolCallID, ToolName: part.ToolName, Result: part.Result, IsError: part.IsError}, includeThoughts)
		if err != nil {
			return nil, false, err
		}
		s.content = append(s.content, converted)
		return partial(converted), false, nil
	case "file", "reasoning-file":
		converted, err := convertOutputPart(protocol.OutputPart{Type: part.Type, MediaType: part.MediaType, Data: part.Data}, includeThoughts)
		if err != nil {
			return nil, false, err
		}
		s.content = append(s.content, converted)
		return partial(converted), false, nil
	case "finish":
		response := &model.LLMResponse{Content: &genai.Content{Role: string(genai.RoleModel), Parts: s.content}, FinishReason: finishReason(part.FinishReason), UsageMetadata: usageMetadata(part.Usage), ModelVersion: s.modelID, TurnComplete: true}
		attachMetadata(response, part.ProviderMetadata, part.Usage)
		parsed, _ := adkmodels.MetadataFromResponse(response)
		parsed.ResponseID = s.responseID
		metadata.Attach(response, parsed)
		return response, true, nil
	case "error":
		raw, _ := json.Marshal(part.Error)
		return nil, false, fmt.Errorf("vercel: stream error: %s", raw)
	default:
		return nil, false, fmt.Errorf("%w %q", ErrUnknownStreamPart, part.Type)
	}
}

func (s *streamState) startText(id string) *genai.Part {
	if text := s.textParts[id]; text != nil {
		return text
	}
	text := &genai.Part{}
	s.textParts[id] = text
	s.content = append(s.content, text)
	return text
}

func mergeMetadata(values ...map[string]any) map[string]any {
	var result map[string]any
	for _, value := range values {
		for namespace, raw := range value {
			if result == nil {
				result = make(map[string]any)
			}
			incoming, incomingIsMap := raw.(map[string]any)
			current, currentIsMap := result[namespace].(map[string]any)
			if !incomingIsMap || !currentIsMap {
				result[namespace] = raw
				continue
			}
			merged := make(map[string]any, len(current)+len(incoming))
			for key, item := range current {
				merged[key] = item
			}
			for key, item := range incoming {
				merged[key] = item
			}
			result[namespace] = merged
		}
	}
	return result
}

func partial(part *genai.Part) *model.LLMResponse {
	return &model.LLMResponse{Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{part}}, Partial: true}
}

func readSSE(reader io.Reader, consume func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		joined := []byte(strings.Join(data, "\n"))
		data = nil
		return consume(joined)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("vercel: read stream: %w", err)
	}
	return flush()
}
