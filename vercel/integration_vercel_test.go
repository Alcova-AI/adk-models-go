// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestIntegrationVercelNativeLuna(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	llm, err := NewModel(Config{APIKey: apiKey, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", PromptCaching: adkmodels.PromptCachingConfig{OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheExplicit, Key: "adk-vercel-integration", SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}, ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}}}}})

	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("Reply with exactly native-vercel-ok", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("Follow the user response format exactly.", "system"),
			ThinkingConfig:    &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow},
			MaxOutputTokens:   64,
		},
	}
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			var text strings.Builder
			var complete bool
			for response, err := range llm.GenerateContent(t.Context(), request, stream) {
				if err != nil {
					t.Fatalf("GenerateContent() error = %v", err)
				}
				complete = complete || response.TurnComplete
				if response.Content != nil {
					for _, part := range response.Content.Parts {
						if part != nil && !part.Thought {
							text.WriteString(part.Text)
						}
					}
				}
			}
			if !strings.Contains(strings.ToLower(text.String()), "native-vercel-ok") {
				t.Fatalf("response text = %q", text.String())
			}
			if !complete {
				t.Fatal("response did not complete")
			}
		})
	}
}

func TestIntegrationVercelNativeGatewayRoutingOptions(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	tests := []struct {
		name    string
		options adkmodels.VercelConfig
	}{
		{name: "zdr", options: adkmodels.VercelConfig{ZeroDataRetention: true}},
		{name: "only azure", options: adkmodels.VercelConfig{Only: []string{"azure"}}},
		{name: "order azure", options: adkmodels.VercelConfig{Order: []string{"azure"}}},
		{name: "only azure zdr", options: adkmodels.VercelConfig{Only: []string{"azure"}, ZeroDataRetention: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			llm, err := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &test.options}, APIKey: apiKey})
			if err != nil {
				t.Fatalf("NewModel() error = %v", err)
			}
			request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Reply with OK.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 16}}
			for _, err := range llm.GenerateContent(t.Context(), request, false) {
				if err != nil {
					t.Fatalf("GenerateContent() error = %v", err)
				}
			}
		})
	}
}

func TestIntegrationVercelNativeImageAndPDF(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	llm, err := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna"}, APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	const pdfVerificationCode = "VERCEL-PDF-482917"
	request := &model.LLMRequest{
		Contents: []*genai.Content{{Role: string(genai.RoleUser), Parts: []*genai.Part{
			{Text: "Read the attached PDF and return only the verification code printed in it. Ignore the image."},
			{InlineData: &genai.Blob{Data: png, MIMEType: "image/png", DisplayName: "pixel.png"}},
			{InlineData: &genai.Blob{Data: minimalPDF("Verification code: " + pdfVerificationCode), MIMEType: "application/pdf", DisplayName: "sample.pdf"}},
		}}},
		Config: &genai.GenerateContentConfig{MaxOutputTokens: 64, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}},
	}
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			var text strings.Builder
			for response, err := range llm.GenerateContent(t.Context(), request, stream) {
				if err != nil {
					t.Fatalf("GenerateContent() error = %v", err)
				}
				if response.Content == nil {
					continue
				}
				for _, part := range response.Content.Parts {
					if part != nil && !part.Thought {
						text.WriteString(part.Text)
					}
				}
			}
			if !strings.Contains(text.String(), pdfVerificationCode) {
				t.Fatalf("response text = %q", text.String())
			}
		})
	}
}

func minimalPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	var result bytes.Buffer
	result.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = result.Len()
		fmt.Fprintf(&result, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := result.Len()
	fmt.Fprintf(&result, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&result, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&result, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return result.Bytes()
}

func TestIntegrationVercelNativeParallelTools(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	llm, err := NewModel(Config{Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna"}, APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("Call lookup once for id 1 and once for id 2. Make both calls before answering.", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: 512,
			ThinkingConfig:  &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow},
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "lookup", Description: "Look up one numeric identifier.",
				Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"id": {Type: genai.TypeInteger}}, Required: []string{"id"}},
			}}}},
			ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}},
		},
	}
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			var calls []*genai.FunctionCall
			var assistantParts []*genai.Part
			for response, err := range llm.GenerateContent(t.Context(), request, stream) {
				if err != nil {
					t.Fatalf("GenerateContent() error = %v", err)
				}
				if response.Content == nil {
					continue
				}
				for _, part := range response.Content.Parts {
					if part == nil {
						continue
					}
					assistantParts = append(assistantParts, part)
					if part.FunctionCall != nil {
						calls = append(calls, part.FunctionCall)
					}
				}
			}
			if len(calls) != 2 {
				t.Fatalf("tool call count = %d, want 2", len(calls))
			}
			if calls[0].ID == "" || calls[1].ID == "" || calls[0].ID == calls[1].ID {
				t.Fatalf("tool call IDs = %q, %q", calls[0].ID, calls[1].ID)
			}

			followUp := &model.LLMRequest{
				Contents: []*genai.Content{
					request.Contents[0],
					{Role: string(genai.RoleModel), Parts: assistantParts},
					{Role: string(genai.RoleUser), Parts: []*genai.Part{
						{FunctionResponse: &genai.FunctionResponse{ID: calls[0].ID, Name: calls[0].Name, Response: map[string]any{"value": "alpha"}}},
						{FunctionResponse: &genai.FunctionResponse{ID: calls[1].ID, Name: calls[1].Name, Response: map[string]any{"value": "beta"}}},
					}},
				},
				Config: &genai.GenerateContentConfig{
					MaxOutputTokens: 256,
					ThinkingConfig:  &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow},
					Tools:           request.Config.Tools,
				},
			}
			var answer strings.Builder
			for response, err := range llm.GenerateContent(t.Context(), followUp, stream) {
				if err != nil {
					t.Fatalf("tool result follow-up error = %v", err)
				}
				if response.Content == nil {
					continue
				}
				for _, part := range response.Content.Parts {
					if part != nil && !part.Thought {
						answer.WriteString(part.Text)
					}
				}
			}
			lowerAnswer := strings.ToLower(answer.String())
			if !strings.Contains(lowerAnswer, "alpha") || !strings.Contains(lowerAnswer, "beta") {
				t.Fatalf("tool result answer = %q", answer.String())
			}
		})
	}
}
