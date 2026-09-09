// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package openai

import (
	"encoding/json"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
)

func TestExplicitCachingKeepsRollingConversationBoundaries(t *testing.T) {
	request := protocol.CallOptions{Prompt: []protocol.Message{
		{Role: "system", Content: "prompt"},
		textMessage("user", "message A"),
		textMessage("assistant", "response B"),
		textMessage("user", "message C"),
		textMessage("assistant", "response D"),
		textMessage("user", "message E"),
		{Role: "user", Content: []protocol.Part{
			{Type: "text", Text: "runtime files"},
			{Type: "file", MediaType: "application/pdf", Data: &protocol.FileData{Type: "data", Data: "cGRm"}},
		}},
	}, ProviderOptions: protocol.ProviderOptions{"openai": {
		"store": false, "reasoningSummary": "auto",
	}}}
	options := Options{
		PromptCaching: adkmodels.OpenAIPromptCachingConfig{
			Mode: adkmodels.OpenAIPromptCacheExplicit, Key: "session-agent",
			SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}, ConversationHistory: &adkmodels.OpenAICacheBreakpoint{},
		},
	}
	if err := options.Apply(&request); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	jsonText := string(raw)
	for _, fragment := range []string{
		`"promptCacheOptions":{"mode":"explicit","ttl":"30m"}`,
		`"promptCacheKey":"session-agent"`,
		`"reasoningSummary":"auto"`,
		`"store":false`,
	} {
		if !strings.Contains(jsonText, fragment) {
			t.Errorf("request missing %s: %s", fragment, jsonText)
		}
	}
	if got := strings.Count(jsonText, `"promptCacheBreakpoint":{"mode":"explicit"}`); got != 4 {
		t.Fatalf("breakpoint count = %d, want 4: %s", got, jsonText)
	}
	for _, text := range []string{"message A", "message C", "message E"} {
		if !strings.Contains(jsonText, `"text":"`+text+`","providerOptions":{"openai":{"promptCacheBreakpoint":{"mode":"explicit"}}}`) {
			t.Errorf("rolling breakpoint missing from %q: %s", text, jsonText)
		}
	}
	if strings.Contains(jsonText, `"text":"runtime files","providerOptions"`) {
		t.Errorf("runtime file message was marked: %s", jsonText)
	}
}

func TestImplicitCachingLeavesThreeExplicitSlots(t *testing.T) {
	request := protocol.CallOptions{Prompt: []protocol.Message{
		textMessage("user", "A"), textMessage("user", "B"), textMessage("user", "C"), textMessage("user", "D"),
	}}
	err := (Options{PromptCaching: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheImplicit, ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}}}).Apply(&request)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	raw, _ := json.Marshal(request)
	if got := strings.Count(string(raw), `"promptCacheBreakpoint"`); got != 3 {
		t.Fatalf("breakpoint count = %d, want 3: %s", got, raw)
	}
}

func textMessage(role, text string) protocol.Message {
	return protocol.Message{Role: role, Content: []protocol.Part{{Type: "text", Text: text}}}
}
