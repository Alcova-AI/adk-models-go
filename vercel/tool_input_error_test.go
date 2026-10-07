package adkvercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestMalformedToolInputRetainsPrivateEvidence(t *testing.T) {
	const rawInput = "{\"question\":\"private note\\nwith quotes \\\""
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream-%t", stream), func(t *testing.T) {
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				part := map[string]any{"type": "tool-call", "toolCallId": "call_question", "toolName": "ask_question", "input": rawInput}
				if stream {
					body, _ := json.Marshal(part)
					return jsonResponse(req, "data: "+string(body)+"\n\n"), nil
				}
				body, _ := json.Marshal(map[string]any{"content": []any{part}})
				return jsonResponse(req, string(body)), nil
			})
			llm, err := NewModel(Config{APIKey: "test", HTTPClient: &http.Client{Transport: transport}, Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna"}})
			if err != nil {
				t.Fatal(err)
			}
			var failure error
			for _, err := range llm.GenerateContent(context.Background(), &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("ask", genai.RoleUser)}}, stream) {
				if err != nil {
					failure = fmt.Errorf("wrapped: %w", err)
				}
			}
			var captured *ToolInputError
			if !errors.As(failure, &captured) {
				t.Fatalf("missing tool evidence: %v", failure)
			}
			if captured.RawInput != rawInput || captured.ToolCallID != "call_question" || captured.ToolName != "ask_question" {
				t.Fatal("tool evidence changed")
			}
			var syntax *json.SyntaxError
			if !errors.As(failure, &syntax) {
				t.Fatal("JSON cause lost")
			}
			if strings.Contains(failure.Error(), "private note") {
				t.Fatal("raw arguments leaked into ordinary error")
			}
		})
	}
}
