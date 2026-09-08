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

package adkopenai

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// TestOpenAILiveChangedTailCache is skipped unless OPENAI_API_KEY is set. Each
// pair uses a fresh prefix and key, so a hit on its second request can only
// come from the first request in that pair.
func TestOpenAILiveChangedTailCache(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	runID := time.Now().UnixNano()
	for pair := 1; pair <= 5; pair++ {
		cacheKey := fmt.Sprintf("adk-openai-go-direct-%d-%d", runID, pair)
		cacheModel, err := newTestModel(testConfig{
			Client:         client,
			CanonicalModel: "gpt-5.6-luna",
		},
			withPromptCaching(PromptCachingConfig{
				Mode:                PromptCacheExplicit,
				Key:                 cacheKey,
				ConversationHistory: &CacheBreakpoint{},
			}),
		)
		if err != nil {
			t.Fatalf("newTestModel() error = %v", err)
		}
		stableHistory := genai.NewContentFromText(
			fmt.Sprintf("Direct cache verification run %d pair %d. ", runID, pair)+strings.Repeat("Stable prompt cache context. ", 500),
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
		if usage != nil && usage.CachedContentTokenCount > 0 {
			t.Logf("pair %d first changed-tail request reused %d cached input tokens", pair, usage.CachedContentTokenCount)
			return
		}
		t.Logf("pair %d first changed-tail request missed the cache", pair)
	}
	t.Fatal("no isolated changed-tail pair reported a cache hit")
}

func TestOpenAILiveStreaming(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	streamingModel, err := newTestModel(
		testConfig{Client: client, CanonicalModel: "gpt-5.6-luna"},
		withReasoning(ReasoningConfig{DefaultLevel: genai.ThinkingLevelLow}),
	)
	if err != nil {
		t.Fatalf("newTestModel() error = %v", err)
	}
	request := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("Reply with exactly: direct-stream-ok", genai.RoleUser)},
		Config:   &genai.GenerateContentConfig{MaxOutputTokens: 32},
	}

	var partials int
	var final *model.LLMResponse
	for response, err := range streamingModel.GenerateContent(t.Context(), request, true) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		if response.Partial {
			partials++
		} else {
			final = response
		}
	}
	if partials == 0 {
		t.Fatal("stream did not yield any partial responses")
	}
	if final == nil || !final.TurnComplete || final.Content == nil || len(final.Content.Parts) == 0 {
		t.Fatalf("final response = %#v", final)
	}
	if got := final.Content.Parts[len(final.Content.Parts)-1].Text; got != "direct-stream-ok" {
		t.Fatalf("final response text = %q, want direct-stream-ok", got)
	}
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
