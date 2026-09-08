// Copyright 2026 Alcova AI
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adkopenai_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	"google.golang.org/adk/v2/model"
)

// TestVercelLive is skipped unless AI_GATEWAY_API_KEY is set. It is kept in
// the repository so gateway compatibility can be checked again after SDK or
// API changes without making normal unit tests depend on network access.
func TestVercelLive(t *testing.T) {
	apiKey := os.Getenv("AI_GATEWAY_API_KEY")
	if apiKey == "" {
		t.Skip("AI_GATEWAY_API_KEY is not set")
	}
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL("https://ai-gateway.vercel.sh/v1"),
	)
	llm, err := adkopenai.NewModel(adkopenai.Config{Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{}}})

	if err != nil {
		t.Fatalf("adkopenai.NewModel() error = %v", err)
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("Reply with exactly: adapter-ok", genai.RoleUser)},
		Config:   &genai.GenerateContentConfig{MaxOutputTokens: 32},
	}
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocking", true: "streaming"}[stream], func(t *testing.T) {
			var final *model.LLMResponse
			for response, err := range llm.GenerateContent(t.Context(), request, stream) {
				if err != nil {
					t.Fatalf("GenerateContent() error = %v", err)
				}
				if !response.Partial {
					final = response
				}
			}
			if final == nil || final.Content == nil || len(final.Content.Parts) == 0 || final.Content.Parts[0].Text != "adapter-ok" {
				t.Fatalf("final response = %#v", final)
			}
		})
	}

	t.Run("file", func(t *testing.T) {
		fileRequest := &model.LLMRequest{
			Contents: []*genai.Content{{
				Role: string(genai.RoleUser),
				Parts: []*genai.Part{
					{Text: "Read the attached PDF. Reply with only its visible title."},
					{FileData: &genai.FileData{
						FileURI:  "https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf",
						MIMEType: "application/pdf",
					}},
				},
			}},
			Config: &genai.GenerateContentConfig{MaxOutputTokens: 32},
		}
		final := collectLiveFinal(t, llm, fileRequest)
		if got := final.Content.Parts[0].Text; !strings.Contains(got, "Dummy PDF file") {
			t.Fatalf("file response = %q", got)
		}
	})

	t.Run("tool", func(t *testing.T) {
		toolRequest := &model.LLMRequest{
			Contents: []*genai.Content{genai.NewContentFromText("Call echo_code with code gateway-tool-ok.", genai.RoleUser)},
			Config: &genai.GenerateContentConfig{
				MaxOutputTokens: 64,
				Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
					Name:        "echo_code",
					Description: "Echo a verification code",
					Parameters: &genai.Schema{
						Type:     genai.TypeObject,
						Required: []string{"code"},
						Properties: map[string]*genai.Schema{
							"code": {Type: genai.TypeString},
						},
					},
				}}}},
				ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode:                 genai.FunctionCallingConfigModeAny,
					AllowedFunctionNames: []string{"echo_code"},
				}},
			},
		}
		final := collectLiveFinal(t, llm, toolRequest)
		call := final.Content.Parts[0].FunctionCall
		if call == nil || call.Name != "echo_code" || call.Args["code"] != "gateway-tool-ok" {
			t.Fatalf("function call = %#v", call)
		}
	})

	t.Run("reasoning-replay", func(t *testing.T) {
		reasoningModel, err := adkopenai.NewModel(adkopenai.Config{Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-sol", RequestModel: "openai/gpt-5.6-sol", Vercel: &adkmodels.VercelConfig{}}})
		if err != nil {
			t.Fatalf("adkopenai.NewModel() reasoning route error = %v", err)
		}
		reasoningRequest := &model.LLMRequest{
			Contents: []*genai.Content{genai.NewContentFromText("Solve carefully: three boxes are labeled Apples, Oranges, and Mixed, but every label is wrong. You may draw one fruit from one box. Explain how to relabel all boxes.", genai.RoleUser)},
			Config: &genai.GenerateContentConfig{
				MaxOutputTokens: 512,
				ThinkingConfig: &genai.ThinkingConfig{
					ThinkingLevel:   genai.ThinkingLevelLow,
					IncludeThoughts: true,
				},
			},
		}
		first := collectLiveFinal(t, reasoningModel, reasoningRequest)
		var hasReasoningState bool
		for _, part := range first.Content.Parts {
			if part.Thought && len(part.ThoughtSignature) > 0 {
				hasReasoningState = true
			}
		}
		if !hasReasoningState {
			t.Fatalf("response did not contain replayable reasoning state: %#v", first.Content.Parts)
		}
		followUp := &model.LLMRequest{
			Contents: []*genai.Content{
				reasoningRequest.Contents[0],
				first.Content,
				genai.NewContentFromText("Now summarize the method in one sentence.", genai.RoleUser),
			},
			Config: reasoningRequest.Config,
		}
		_ = collectLiveFinal(t, reasoningModel, followUp)
	})

	t.Run("explicit-cache-changed-tail", func(t *testing.T) {
		runID := time.Now().UnixNano()
		cacheKey := fmt.Sprintf("adk-openai-go-live-%d", runID)
		for pair := 1; pair <= 5; pair++ {
			cacheModel, err := adkopenai.NewModel(adkopenai.Config{Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "openai/gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{}, PromptCaching: adkmodels.PromptCachingConfig{OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheExplicit, Key: cacheKey, ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}}}}})
			if err != nil {
				t.Fatalf("adkopenai.NewModel() cache route error = %v", err)
			}
			stableHistory := genai.NewContentFromText(
				fmt.Sprintf("Cache verification run %d pair %d. ", runID, pair)+strings.Repeat("Stable conversation cache context. ", 500),
				genai.RoleUser,
			)
			firstRequest := &model.LLMRequest{
				Contents: []*genai.Content{
					stableHistory,
					genai.NewContentFromText("Stable assistant acknowledgement.", genai.RoleModel),
					genai.NewContentFromText("Reply with exactly: first-tail", genai.RoleUser),
				},
				Config: &genai.GenerateContentConfig{MaxOutputTokens: 32},
			}
			_ = collectLiveFinal(t, cacheModel, firstRequest)

			changedTailRequest := &model.LLMRequest{
				Contents: []*genai.Content{
					stableHistory,
					genai.NewContentFromText("Stable assistant acknowledgement.", genai.RoleModel),
					genai.NewContentFromText("Reply with exactly: changed-tail", genai.RoleUser),
				},
				Config: firstRequest.Config,
			}
			second := collectLiveFinal(t, cacheModel, changedTailRequest)
			usage := second.UsageMetadata
			if metadata, ok := adkmodels.MetadataFromResponse(second); ok {
				t.Logf("pair %d resolved provider: %s", pair, metadata.ResolvedProvider)
			}
			if usage != nil && usage.CachedContentTokenCount > 0 {
				t.Logf("pair %d first changed-tail request reused %d cached input tokens", pair, usage.CachedContentTokenCount)
				return
			}
			t.Logf("pair %d first changed-tail request missed the cache", pair)
		}
		t.Fatal("no isolated changed-tail pair reported a cache hit")
	})
}

func collectLiveFinal(t *testing.T, llm model.LLM, request *model.LLMRequest) *model.LLMResponse {
	t.Helper()
	var final *model.LLMResponse
	for response, err := range llm.GenerateContent(t.Context(), request, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		final = response
	}
	if final == nil || final.Content == nil || len(final.Content.Parts) == 0 {
		t.Fatalf("empty final response: %#v", final)
	}
	return final
}
