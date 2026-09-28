// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package converters

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/openai/openai-go/v3"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestChatMedia(t *testing.T) {
	for _, tc := range []struct {
		name string
		part *genai.Part
		want string
		bad  bool
	}{
		{"inline image", &genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1}}}, "image_url", false},
		{"image URL", &genai.Part{FileData: &genai.FileData{MIMEType: "image/png", FileURI: "https://example.com/image.png"}}, "image_url", false},
		{"inline file", &genai.Part{InlineData: &genai.Blob{MIMEType: "application/pdf", DisplayName: "test.pdf", Data: []byte{1}}}, "file_data", false},
		{"uploaded file", &genai.Part{FileData: &genai.FileData{MIMEType: "application/pdf", FileURI: "file-123"}}, "file_id", false},
		{"file URL", &genai.Part{FileData: &genai.FileData{MIMEType: "application/pdf", FileURI: "https://example.com/file.pdf"}}, "", true},
		{"empty bytes", &genai.Part{InlineData: &genai.Blob{MIMEType: "image/png"}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := LLMRequestToChatParams("gpt-6-luna", &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{tc.part}}}}, 100)
			if (err != nil) != tc.bad {
				t.Fatalf("error=%v", err)
			}
			if !tc.bad {
				raw, _ := json.Marshal(p)
				if !strings.Contains(string(raw), tc.want) {
					t.Fatalf("missing %s: %s", tc.want, raw)
				}
			}
		})
	}
}
func TestChatParallelToolResults(t *testing.T) {
	r := &model.LLMRequest{Contents: []*genai.Content{
		{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "one", Name: "lookup"}}, {FunctionCall: &genai.FunctionCall{ID: "two", Name: "lookup"}}}},
		{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "two", Name: "lookup"}}, {FunctionResponse: &genai.FunctionResponse{ID: "one", Name: "lookup"}}}},
	}}
	p, err := LLMRequestToChatParams("gpt-6-luna", r, 100)
	if err != nil {
		t.Fatal(err)
	}
	if p.Messages[1].OfTool.ToolCallID != "two" || p.Messages[2].OfTool.ToolCallID != "one" {
		t.Fatal("reordered tool results")
	}
	r.Contents = r.Contents[:1]
	if _, err := LLMRequestToChatParams("gpt-6-luna", r, 100); err == nil {
		t.Fatal("accepted missing results")
	}
}
func TestChatReplyValidationAndUsage(t *testing.T) {
	raw := `{"choices":[{"message":{"refusal":"Cannot comply"},"finish_reason":"content_filter"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":3}}}`
	var reply sdk.ChatCompletion
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	r, err := ChatToLLMResponse(&reply)
	if err != nil {
		t.Fatal(err)
	}
	if r.FinishReason != genai.FinishReasonSafety || r.Content.Parts[0].Text != "Cannot comply" || r.UsageMetadata.CachedContentTokenCount != 5 || r.UsageMetadata.ThoughtsTokenCount != 3 {
		t.Fatalf("lost reply facts: %+v", r)
	}
	for _, args := range []string{"[]", "null", "broken"} {
		raw := `{"choices":[{"message":{"tool_calls":[{"id":"x","type":"function","function":{"name":"lookup","arguments":` + string(mustJSON(t, args)) + `}}]},"finish_reason":"tool_calls"}]}`
		json.Unmarshal([]byte(raw), &reply)
		if _, err := ChatToLLMResponse(&reply); err == nil {
			t.Errorf("accepted arguments %s", args)
		}
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
