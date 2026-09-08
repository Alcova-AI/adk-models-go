// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package openai_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
)

func TestIntegrationVercelNativeImplicitHistoryCacheDataPolicy(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	tests := []struct {
		name string
		zdr  bool
	}{
		{name: "azure"},
		{name: "azure zdr", zdr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			llm, err := adkvercel.NewModel(adkvercel.Config{APIKey: apiKey, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{Only: []string{"azure"}, ZeroDataRetention: test.zdr}}})
			if err != nil {
				t.Fatalf("adkvercel.NewModel() error = %v", err)
			}
			stableSentence := fmt.Sprintf(
				"This is the stable text-only conversation prefix for the %s implicit cache test run %d. ",
				test.name,
				time.Now().UnixNano(),
			)
			stablePrefix := strings.Repeat(stableSentence, 700)
			baseContents := []*genai.Content{
				genai.NewContentFromText(stablePrefix, genai.RoleUser),
				genai.NewContentFromText("I have read and retained the stable conversation prefix.", genai.RoleModel),
				genai.NewContentFromText("First user message after the stable prefix. Reply with OK.", genai.RoleUser),
			}
			requestFor := func(contents []*genai.Content) *model.LLMRequest {
				return &model.LLMRequest{
					Contents: contents,
					Config:   &genai.GenerateContentConfig{MaxOutputTokens: 16},
				}
			}
			cacheRead := func(request *model.LLMRequest) int32 {
				var cached int32
				for response, err := range llm.GenerateContent(t.Context(), request, false) {
					if err != nil {
						t.Fatalf("GenerateContent() error = %v", err)
					}
					if response.UsageMetadata != nil {
						cached = response.UsageMetadata.CachedContentTokenCount
					}
				}
				return cached
			}

			_ = cacheRead(requestFor(baseContents))
			appendedContents := append(append([]*genai.Content{}, baseContents...),
				genai.NewContentFromText("Assistant response to the first user message.", genai.RoleModel),
				genai.NewContentFromText("New appended user message. Reply with OK.", genai.RoleUser),
			)
			cached := cacheRead(requestFor(appendedContents))
			if cached == 0 {
				thirdContents := append(append([]*genai.Content{}, appendedContents...),
					genai.NewContentFromText("Assistant response to the new user message.", genai.RoleModel),
					genai.NewContentFromText("Another appended user message. Reply with OK.", genai.RoleUser),
				)
				cached = cacheRead(requestFor(thirdContents))
			}
			if cached == 0 {
				t.Fatal("text-only appended message received zero implicit cached tokens")
			}
			t.Logf("text-only appended message reused %d implicit cached tokens", cached)
		})
	}
}

func TestIntegrationVercelNativeFileHistoryCache(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	llm, err := adkvercel.NewModel(adkvercel.Config{APIKey: apiKey, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{Only: []string{"openai"}}, PromptCaching: adkmodels.PromptCachingConfig{OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheExplicit, Key: "adk-vercel-file-history-cache", ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}}}}})
	if err != nil {
		t.Fatalf("adkvercel.NewModel() error = %v", err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	stableA := strings.Repeat("This stable reference describes account 123, its documented rules, and the required response format. ", 700)
	requestFor := func(final string) *model.LLMRequest {
		return &model.LLMRequest{Contents: []*genai.Content{
			genai.NewContentFromText(stableA, genai.RoleUser),
			genai.NewContentFromText("I have read the stable reference.", genai.RoleModel),
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "Message C includes this image."}, {InlineData: &genai.Blob{Data: png, MIMEType: "image/png", DisplayName: "pixel.png"}}}},
			genai.NewContentFromText("The image is available.", genai.RoleModel),
			genai.NewContentFromText(final, genai.RoleUser),
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "Runtime hydrated file context."}, {InlineData: &genai.Blob{Data: png, MIMEType: "image/png", DisplayName: "runtime.png"}}}},
		}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 32, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}}}
	}
	cacheRead := func(request *model.LLMRequest) int32 {
		var cached int32
		for response, err := range llm.GenerateContent(t.Context(), request, false) {
			if err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}
			if response.UsageMetadata != nil {
				cached = response.UsageMetadata.CachedContentTokenCount
			}
		}
		return cached
	}
	_ = cacheRead(requestFor("Message E, first version. Reply with OK."))
	cached := cacheRead(requestFor("Message E, changed version. Reply with OK."))
	if cached == 0 {
		cached = cacheRead(requestFor("Message E, third version. Reply with OK."))
	}
	if cached == 0 {
		t.Fatal("changed final message received zero cached tokens")
	}
	t.Logf("changed final message reused %d cached tokens", cached)
}

func TestIntegrationVercelNativeHistoryCacheAfterFile(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	llm, err := adkvercel.NewModel(adkvercel.Config{APIKey: apiKey, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{Only: []string{"openai"}}, PromptCaching: adkmodels.PromptCachingConfig{OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheExplicit, Key: "adk-vercel-history-after-file", ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}}}}})
	if err != nil {
		t.Fatalf("adkvercel.NewModel() error = %v", err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	stableAfterFile := strings.Repeat("This stable message follows the uploaded file and must remain cacheable across later user messages. ", 700)
	requestFor := func(final string) *model.LLMRequest {
		return &model.LLMRequest{Contents: []*genai.Content{
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "Message C includes this image."}, {InlineData: &genai.Blob{Data: png, MIMEType: "image/png", DisplayName: "pixel.png"}}}},
			genai.NewContentFromText("The image is available.", genai.RoleModel),
			genai.NewContentFromText(stableAfterFile, genai.RoleUser),
			genai.NewContentFromText("I have read the stable message.", genai.RoleModel),
			genai.NewContentFromText(final, genai.RoleUser),
			{Role: string(genai.RoleUser), Parts: []*genai.Part{{Text: "Runtime hydrated file context."}, {InlineData: &genai.Blob{Data: png, MIMEType: "image/png", DisplayName: "runtime.png"}}}},
		}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 32, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}}}
	}
	cacheRead := func(request *model.LLMRequest) int32 {
		var cached int32
		for response, err := range llm.GenerateContent(t.Context(), request, false) {
			if err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}
			if response.UsageMetadata != nil {
				cached = response.UsageMetadata.CachedContentTokenCount
			}
		}
		return cached
	}
	_ = cacheRead(requestFor("Message G, first version. Reply with OK."))
	cached := cacheRead(requestFor("Message G, changed version. Reply with OK."))
	if cached == 0 {
		cached = cacheRead(requestFor("Message G, third version. Reply with OK."))
	}
	if cached == 0 {
		t.Fatal("history after file received zero cached tokens")
	}
	t.Logf("history after file reused %d cached tokens", cached)
}
