// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"encoding/json"
	"google.golang.org/genai"
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

func TestEmptyStreamTextMessageMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, metadataEvent string
		events              []string
	}{
		{"no-delta/start", "text-start", []string{"text-start", "text-end"}},
		{"no-delta/end", "text-end", []string{"text-start", "text-end"}},
		{"empty-delta/start", "text-start", []string{"text-start", "text-delta", "text-end"}},
		{"empty-delta/delta", "text-delta", []string{"text-start", "text-delta", "text-end"}},
		{"empty-delta/end", "text-end", []string{"text-start", "text-delta", "text-end"}},
	} {
		for _, phase := range []string{"commentary", "final_answer"} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				state := newStreamState()
				for _, kind := range tc.events {
					event := protocol.StreamPart{Type: kind, ID: "text_empty"}
					if kind == tc.metadataEvent {
						event.ProviderMetadata = map[string]any{"openai": map[string]any{"itemId": "msg_empty", "phase": phase}}
					}
					response, terminal, err := state.convert(event, false)
					if err != nil || response != nil || terminal {
						t.Fatalf("empty %s emitted response=%+v terminal=%t error=%v", kind, response, terminal, err)
					}
				}
				if len(state.content) != 1 || state.content[0].Text != "" {
					t.Fatal("empty text block lost")
				}
				before, err := json.Marshal(state.content)
				if err != nil {
					t.Fatal(err)
				}
				var saved []*genai.Part
				if err := json.Unmarshal(before, &saved); err != nil {
					t.Fatal(err)
				}
				metadata, ok := converters.MessageMetadataFromPart(saved[0])
				if !ok || metadata.ID != "msg_empty" || metadata.Phase != phase {
					t.Fatalf("metadata=%+v", metadata)
				}
				messages, err := convertContents([]*genai.Content{{Role: "model", Parts: saved}})
				if err != nil {
					t.Fatal(err)
				}
				if len(messages) != 1 {
					t.Fatal("missing replay message")
				}
				part := messages[0].Content.([]protocol.Part)[0]
				if part.Text != "" || part.ProviderOptions["openai"]["phase"] != phase || part.ProviderOptions["openai"]["itemId"] != "msg_empty" {
					t.Fatalf("replay=%+v", part)
				}
			})
		}
	}
}
