// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"encoding/json"
	"sync"
	"testing"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

func TestStreamPartialMetadataIsSnapshot(t *testing.T) {
	for _, eventType := range []string{"text-delta", "text-end"} {
		t.Run(eventType, func(t *testing.T) {
			state := newStreamState()
			_, _, err := state.convert(protocol.StreamPart{Type: "text-start", ID: "text_1", ProviderMetadata: map[string]any{"openai": map[string]any{"phase": "final_answer"}}}, false)
			if err != nil {
				t.Fatal(err)
			}
			first, _, err := state.convert(protocol.StreamPart{Type: "text-delta", ID: "text_1", Delta: "{}"}, false)
			if err != nil || first == nil || !first.Partial {
				t.Fatalf("first=%#v error=%v", first, err)
			}
			before, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = state.convert(protocol.StreamPart{Type: eventType, ID: "text_1", Delta: " ", ProviderMetadata: map[string]any{"openai": map[string]any{"itemId": "msg_final"}}}, false)
			if err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatalf("delivered partial changed: before=%s after=%s", before, after)
			}
			metadata, ok := converters.MessageMetadataFromPart(state.content[0])
			if !ok || metadata.ID != "msg_final" || metadata.Phase != "final_answer" {
				t.Fatalf("accumulated metadata=%#v", metadata)
			}
			first.Content.Parts[0].PartMetadata["consumer"] = "private"
			if _, ok := state.content[0].PartMetadata["consumer"]; ok {
				t.Fatal("consumer mutation reached accumulator")
			}
		})
	}
}

func TestStreamPartialMetadataConcurrentRead(t *testing.T) {
	state := newStreamState()
	_, _, err := state.convert(protocol.StreamPart{Type: "text-start", ID: "text_1", ProviderMetadata: map[string]any{"openai": map[string]any{"phase": "final_answer"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := state.convert(protocol.StreamPart{Type: "text-delta", ID: "text_1", Delta: "{}"}, false)
	if err != nil || first == nil {
		t.Fatalf("first=%#v error=%v", first, err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			if _, err := json.Marshal(first); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	close(start)
	for range 1000 {
		_, _, err := state.convert(protocol.StreamPart{Type: "text-delta", ID: "text_1", Delta: " ", ProviderMetadata: map[string]any{"openai": map[string]any{"itemId": "msg_final"}}}, false)
		if err != nil {
			t.Error(err)
			break
		}
	}
	wg.Wait()
}
