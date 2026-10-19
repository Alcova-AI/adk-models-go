// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkmodels_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/toolschema"
)

// Paid, synthetic provider checks. No tools execute and no customer data is used.
func TestStrictToolTranslationVercelLive(t *testing.T) {
	if os.Getenv("ADK_STRICT_LIVE") != "1" {
		t.Skip("set ADK_STRICT_LIVE=1 for paid synthetic calls")
	}
	if os.Getenv("AI_GATEWAY_API_KEY") == "" {
		t.Fatal("AI_GATEWAY_API_KEY required")
	}
	output := os.Getenv("ADK_STRICT_OUTPUT")
	if output == "" {
		t.Fatal("ADK_STRICT_OUTPUT required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"},"count":{"type":"integer"},"enabled":{"type":"boolean"},"explicit":{"type":["string","null"]},"nested":{"$ref":"#/$defs/Row"}},"required":["label","explicit"],"$defs":{"Row":{"type":"object","additionalProperties":false,"properties":{"quantity":{"type":"integer"}}}}}`)
	routes := []matrixRoute{{"haiku-direct", "anthropic", "direct", "claude-haiku-5-5", "claude-haiku-5-5"}, {"luna-native", "openai", "vercel-native", "gpt-5.6-luna", "openai/gpt-5.6-luna"}, {"luna-responses", "openai", "vercel-openai", "gpt-5.6-luna", "openai/gpt-5.6-luna"}, {"haiku-messages", "anthropic", "vercel-anthropic", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"}, {"haiku-native", "anthropic", "vercel-native", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"}}
	for _, route := range routes {
		for _, stream := range []bool{false, true} {
			for _, wanted := range []string{`{"label":"probe","explicit":null}`, `{"label":"probe","explicit":null,"count":0,"enabled":false,"nested":{"quantity":0}}`} {
				name := fmt.Sprintf("%s/stream-%t/fields-%t", route.Name, stream, len(wanted) > 50)
				t.Run(name, func(t *testing.T) {
					if route.Path == "direct" && os.Getenv("ANTHROPIC_API_KEY") == "" {
						t.Skip("ANTHROPIC_API_KEY required for direct Anthropic live testing")
					}
					capture := &strictLiveResponseCapture{base: http.DefaultTransport}
					tr := &matrixTransport{base: capture}
					mc := adkmodels.ModelConfig{CanonicalModel: route.Model, RequestModel: route.Request, DefaultMaxOutputTokens: 2048, ToolSchemas: toolschema.Config{}, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelLow}, Vercel: &adkmodels.VercelConfig{Only: []string{route.Provider}, ZeroDataRetention: true}}
					if route.Path == "direct" {
						mc.Vercel = nil
					}
					ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
					defer cancel()
					llm, err := matrixConfiguredModel(ctx, route, toolschema.Config{}, tr, mc, 90*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					request := &model.LLMRequest{Model: route.Request, Contents: []*genai.Content{genai.NewContentFromText("Call schema_probe exactly once with these arguments: "+wanted+". Omit any property absent from that object. No action will execute.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "schema_probe", Description: "Synthetic recorder; never executes", ParametersJsonSchema: schema}}}}, ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"schema_probe"}}}}}
					var args map[string]any
					var metadata adkmodels.Metadata
					for response, err := range llm.GenerateContent(ctx, request, stream) {
						if err != nil {
							t.Fatal(err)
						}
						if response != nil && !response.Partial {
							if m, ok := adkmodels.MetadataFromResponse(response); ok {
								metadata = m
							}
							if response.Content != nil {
								for _, part := range response.Content.Parts {
									if part.FunctionCall != nil {
										if part.FunctionCall.Name != "schema_probe" {
											t.Fatalf("wrong tool %s", part.FunctionCall.Name)
										}
										args = part.FunctionCall.Args
									}
								}
							}
						}
					}
					var expected map[string]any
					if err := json.Unmarshal([]byte(wanted), &expected); err != nil {
						t.Fatal(err)
					}
					evidence := map[string]any{"route": route, "stream": stream, "expected": expected, "actual": args, "raw_tool_calls": capture.calls, "schemas": tr.schemas, "http_statuses": tr.statuses, "resolved_provider": metadata.ResolvedProvider, "generation_id": metadata.GenerationID, "cost_usd": metadata.CostUSD, "transport": route.Path, "gateway_zdr_requested": mc.Vercel != nil && mc.Vercel.ZeroDataRetention}
					if mc.Vercel != nil {
						evidence["only"] = mc.Vercel.Only
					} else {
						evidence["direct_account_retention"] = "unverified"
					}
					data, err := json.MarshalIndent(evidence, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					filename := fmt.Sprintf("%s-stream-%t-fields-%t.json", route.Name, stream, len(wanted) > 50)
					if err := os.WriteFile(filepath.Join(output, filename), data, 0600); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(args, expected) {
						t.Fatalf("argument mismatch: got%v want%v", args, expected)
					}
					if len(tr.schemas) == 0 {
						t.Fatal("no wire schema captured")
					}
					for _, captured := range tr.schemas {
						if captured.(map[string]any)["strict"] != true {
							t.Fatalf("wire did not enable strict: %v", captured)
						}
					}
					if metadata.ResolvedProvider != "" && metadata.ResolvedProvider != route.Provider {
						t.Fatalf("wrong provider %s", metadata.ResolvedProvider)
					}
				})
			}
		}
	}
}

// Capture synthetic tool arguments before adapter decoding, never headers or
// reasoning content. The original response bytes are returned unchanged.
type strictLiveResponseCapture struct {
	base  http.RoundTripper
	calls []map[string]any
}

func (c *strictLiveResponseCapture) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if parts, ok := obj["content"].([]any); ok {
			for _, part := range parts {
				if call, ok := part.(map[string]any); ok && (call["type"] == "tool-call" || call["type"] == "tool_use") {
					c.calls = append(c.calls, call)
				}
			}
		}
	}
	blocks := map[float64]map[string]any{}
	inputs := map[float64]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var call map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &call) != nil {
			continue
		}
		if call["type"] == "tool-call" {
			c.calls = append(c.calls, call)
		}
		index, _ := call["index"].(float64)
		switch call["type"] {
		case "content_block_start":
			block, _ := call["content_block"].(map[string]any)
			if block["type"] == "tool_use" {
				blocks[index] = block
			}
		case "content_block_delta":
			delta, _ := call["delta"].(map[string]any)
			if delta["type"] == "input_json_delta" {
				fragment, _ := delta["partial_json"].(string)
				inputs[index] += fragment
			}
		case "content_block_stop":
			if block := blocks[index]; block != nil {
				if input := inputs[index]; input != "" {
					var args any
					if json.Unmarshal([]byte(input), &args) == nil {
						block["input"] = args
					}
				}
				c.calls = append(c.calls, block)
				delete(blocks, index)
			}
		}
	}
	return response, nil
}
