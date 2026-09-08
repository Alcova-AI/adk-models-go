// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPartMarshalJSONPreservesRequiredEmptyReasoningText(t *testing.T) {
	raw, err := json.Marshal(Part{
		Type: "reasoning",
		ProviderOptions: ProviderOptions{
			"azure": {"itemId": "rs_1", "reasoningEncryptedContent": "opaque"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(raw), `"text":""`) {
		t.Fatalf("reasoning JSON = %s, want required empty text field", raw)
	}
}

func TestPartMarshalJSONOmitsTextFromToolCall(t *testing.T) {
	raw, err := json.Marshal(Part{Type: "tool-call", ToolCallID: "call_1", ToolName: "lookup", Input: map[string]any{"id": 1}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(raw), `"text"`) {
		t.Fatalf("tool call JSON = %s, want no text field", raw)
	}
}
