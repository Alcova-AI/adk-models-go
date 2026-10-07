package adkopenai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestChatSelectedCacheBoundary(t *testing.T) {
	index := 1
	client := sdk.NewClient(option.WithAPIKey("test"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if opts := body["prompt_cache_options"].(map[string]any); opts["mode"] != "implicit" || opts["ttl"] != "30m" {
			t.Fatalf("options: %v", opts)
		}
		if _, ok := body["prompt_cache_key"]; ok {
			t.Fatal("unexpected key")
		}
		parts := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
		if len(parts) != 2 || parts[0].(map[string]any)["text"] != "# Rules\n- Answer only in English." || parts[1].(map[string]any)["text"] != "User: Alice\nDate: today" {
			t.Fatalf("parts: %v", parts)
		}
		if parts[0].(map[string]any)["prompt_cache_breakpoint"].(map[string]any)["mode"] != "explicit" {
			t.Fatal("missing selected marker")
		}
		if _, ok := parts[1].(map[string]any)["prompt_cache_breakpoint"]; ok {
			t.Fatal("marked runtime suffix")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)), Request: r}, nil
	})}))
	cfg := Config{API: APIChatCompletions, Client: client, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", PromptCaching: adkmodels.PromptCachingConfig{SystemInstructionPartIndex: &index, OpenAI: adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheImplicit, SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}}}}}
	llm, err := NewModel(cfg)
	if err != nil {
		t.Fatal(err)
	}
	index = 0 // The constructed model snapshots the selected boundary.
	req := &model.LLMRequest{Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "# Rules"}, {Text: "- Answer only in English."}, {Text: "User: Alice"}, {Text: "Date: today"}}}}, Contents: []*genai.Content{genai.NewContentFromText("Hi", "user")}}
	for _, err := range llm.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(req.Config.SystemInstruction.Parts) != 4 || req.Config.SystemInstruction.Parts[1].Text != "- Answer only in English." {
		t.Fatal("mutated caller request")
	}
}

func TestChatCacheBreakpointLimit(t *testing.T) {
	for _, mode := range []adkmodels.OpenAIPromptCacheMode{adkmodels.OpenAIPromptCacheImplicit, adkmodels.OpenAIPromptCacheExplicit} {
		params := sdk.ChatCompletionNewParams{Messages: []sdk.ChatCompletionMessageParamUnion{sdk.SystemMessage("shared")}}
		for range 8 {
			params.Messages = append(params.Messages, sdk.UserMessage("history"))
		}
		applyChatPromptCaching(&params, adkmodels.OpenAIPromptCachingConfig{Mode: mode, SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}, ConversationHistory: &adkmodels.OpenAICacheBreakpoint{}}, false)
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		want := 4
		if mode == adkmodels.OpenAIPromptCacheImplicit {
			want = 3
		}
		if got := strings.Count(string(raw), "prompt_cache_breakpoint"); got != want {
			t.Fatalf("mode %s: %d markers, want %d", mode, got, want)
		}
	}
}
