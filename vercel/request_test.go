// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"encoding/json"
	"testing"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/Alcova-AI/adk-models-go/internal/protocol"
	"google.golang.org/genai"
)

func TestNativeMessagePhaseHistoryRoundTrip(t *testing.T) {
	var result protocol.GenerateResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"text","text":"checking","providerMetadata":{"openai":{"itemId":"msg_c","phase":"commentary"}}},{"type":"text","text":"answer ","providerMetadata":{"openai":{"itemId":"msg_f","phase":"final_answer"}}},{"type":"text","text":"complete","providerMetadata":{"openai":{"itemId":"msg_f","phase":"final_answer"}}},{"type":"text","text":"another message","providerMetadata":{"openai":{"itemId":"msg_f2","phase":"final_answer"}}}]}`), &result); err != nil {
		t.Fatal(err)
	}
	response, err := convertGenerateResult(result, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response.Content)
	if err != nil {
		t.Fatal(err)
	}
	var content genai.Content
	if err := json.Unmarshal(data, &content); err != nil {
		t.Fatal(err)
	}
	messages, err := convertContents([]*genai.Content{&content})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("want 3 messages, got %#v", messages)
	}
	for i, message := range messages {
		parts := message.Content.([]protocol.Part)
		id, phase, wantText := "msg_c", "commentary", "checking"
		if i == 1 {
			id, phase, wantText = "msg_f", "final_answer", "answer complete"
		}
		if i == 2 {
			id, phase, wantText = "msg_f2", "final_answer", "another message"
		}
		if len(parts) != 1 || parts[0].Text != wantText {
			t.Fatalf("message %d fragments=%#v", i, parts)
		}
		for _, part := range parts {
			options := part.ProviderOptions["openai"]
			if options["itemId"] != id || options["phase"] != phase {
				t.Fatalf("message %d options=%#v", i, options)
			}
		}
	}
	// Check the actual native protocol wire uses providerOptions, not response metadata.
	wire, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	options := decoded[0]["content"].([]any)[0].(map[string]any)["providerOptions"].(map[string]any)["openai"].(map[string]any)
	if options["phase"] != "commentary" {
		t.Fatalf("wire=%s", wire)
	}
}

func TestNativePhaseHistoryPreservesOtherParts(t *testing.T) {
	var result protocol.GenerateResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"reasoning","text":"","providerMetadata":{"openai":{"itemId":"rs_1","reasoningEncryptedContent":"opaque"}}},{"type":"text","text":"checking","providerMetadata":{"openai":{"itemId":"msg_c","phase":"commentary"}}},{"type":"tool-call","toolCallId":"call_1","toolName":"lookup","input":"{}"}]}`), &result); err != nil {
		t.Fatal(err)
	}
	response, err := convertGenerateResult(result, false)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := convertContents([]*genai.Content{response.Content})
	if err != nil {
		t.Fatal(err)
	}
	var parts []protocol.Part
	for _, message := range messages {
		parts = append(parts, message.Content.([]protocol.Part)...)
	}
	if len(parts) != 3 || parts[0].Type != "reasoning" || parts[1].Type != "text" || parts[2].Type != "tool-call" {
		t.Fatalf("order changed: %#v", parts)
	}
	if parts[0].ProviderOptions["openai"]["reasoningEncryptedContent"] != "opaque" || parts[2].ToolCallID != "call_1" {
		t.Fatal("reasoning/tool state changed")
	}
	// An ordinary unphased response retains its existing block structure and gains no provider options.
	messages, err = convertContents([]*genai.Content{{Role: "model", Parts: []*genai.Part{{Text: "one"}, {Text: "two"}}}})
	if err != nil {
		t.Fatal(err)
	}
	parts = messages[0].Content.([]protocol.Part)
	if len(messages) != 1 || len(parts) != 2 || parts[0].ProviderOptions != nil || parts[1].ProviderOptions != nil {
		t.Fatalf("unphased history changed: %#v", messages)
	}
}

func TestNativePhaseHistorySeparatesLegacyAndUserText(t *testing.T) {
	legacy, progress, after := &genai.Part{Text: "legacy"}, &genai.Part{Text: "checking"}, &genai.Part{Text: "after"}
	converters.SetMessageMetadata(progress, converters.MessageMetadata{Phase: "commentary"})
	user := &genai.Part{Text: "question"}
	converters.SetMessageMetadata(user, converters.MessageMetadata{ID: "untrusted", Phase: "final_answer"})
	messages, err := convertContents([]*genai.Content{{Role: "model", Parts: []*genai.Part{legacy, progress, after}}, {Role: "user", Parts: []*genai.Part{user}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("mixed boundaries lost: %#v", messages)
	}
	for _, i := range []int{0, 2, 3} {
		if messages[i].Content.([]protocol.Part)[0].ProviderOptions != nil {
			t.Fatalf("metadata leaked to message %d", i)
		}
	}
	if messages[1].Content.([]protocol.Part)[0].ProviderOptions["openai"]["phase"] != "commentary" {
		t.Fatal("phase-only metadata lost")
	}
}
