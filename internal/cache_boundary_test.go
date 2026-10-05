package internal

import (
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
	"testing"
)

func TestSystemInstructionTexts(t *testing.T) {
	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "one"}, {Text: "two"}, {Text: "three"}}}}}
	for _, i := range []int{-1, 0, 1, 2, 3} {
		texts, err := SystemInstructionTexts(req, &i)
		if i < 0 || i >= 3 {
			if err == nil {
				t.Fatalf("index %d accepted", i)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		joined := ""
		for _, s := range texts {
			joined += s
		}
		if joined != "onetwothree" {
			t.Fatal(texts)
		}
	}
	if texts, err := SystemInstructionTexts(nil, nil); err != nil || texts != nil {
		t.Fatal("legacy path changed")
	}
}

func TestInvalidSystemInstructionBoundary(t *testing.T) {
	index := 0
	for _, req := range []*model.LLMRequest{nil, {}, {Config: &genai.GenerateContentConfig{}}, {Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: ""}}}}}, {Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{InlineData: &genai.Blob{Data: []byte("image")}}}}}}} {
		if _, err := SystemInstructionTexts(req, &index); err == nil {
			t.Fatalf("invalid boundary accepted: %#v", req)
		}
	}
}
