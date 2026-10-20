// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package discovery

import (
	"encoding/json"
	"fmt"
	"iter"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const NativeSearchName = "__adk_native_tool_search"

// OpenAISearchPart retains both item id and client call id for exact replay.
func OpenAISearchPart(raw string) (*genai.Part, error) {
	var o map[string]any
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return nil, err
	}
	if o["execution"] != "client" {
		return nil, fmt.Errorf("native discovery requires client-executed tool search")
	}
	args := o["arguments"]
	if s, ok := args.(string); ok {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			return nil, err
		}
	}
	input := map[string]any{"arguments": args, "call_id": o["call_id"]}
	id := text(o["call_id"])
	if id == "" {
		return nil, fmt.Errorf("native tool search has no client call id")
	}
	return &genai.Part{FunctionCall: &genai.FunctionCall{ID: id, Name: NativeSearchName, Args: input}, PartMetadata: map[string]any{MetadataKey: map[string]any{"provider": "openai", "item_id": o["id"]}}}, nil
}
func Responses(seq iter.Seq2[*model.LLMResponse, error], p *Plan) iter.Seq2[*model.LLMResponse, error] {
	if p == nil {
		return seq
	}
	return func(yield func(*model.LLMResponse, error) bool) {
		for r, err := range seq {
			if err == nil {
				err = p.Annotate(r)
			}
			if !yield(r, err) || err != nil {
				return
			}
		}
	}
}
