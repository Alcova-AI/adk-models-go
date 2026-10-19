// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/tooldiscovery"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

type discoveryLiveRecorder struct {
	model.LLM
	Rows []map[string]any
}

func (m *discoveryLiveRecorder) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if len(m.Rows) > 18 {
			yield(nil, fmt.Errorf("synthetic test exceeded call limit"))
			return
		}
		for r, err := range m.LLM.GenerateContent(ctx, req, stream) {
			if r != nil && !r.Partial {
				row := map[string]any{"usage": r.UsageMetadata}
				meta, _ := adkmodels.MetadataFromResponse(r)
				row["metadata"] = meta
				names := []string{}
				if r.Content != nil {
					for _, p := range r.Content.Parts {
						if p.FunctionCall != nil {
							names = append(names, p.FunctionCall.Name)
						}
					}
				}
				row["calls"] = names
				m.Rows = append(m.Rows, row)
			}
			if !yield(r, err) {
				return
			}
		}
	}
}

// Paid synthetic real-ADK tool loops. No customer data or production writes.
func TestNativeDiscoveryADKLive(t *testing.T) {
	if os.Getenv("ADK_DISCOVERY_LIVE") != "1" {
		t.Skip("set ADK_DISCOVERY_LIVE=1 for paid synthetic calls")
	}
	out := os.Getenv("ADK_DISCOVERY_OUTPUT")
	if out == "" {
		t.Fatal("ADK_DISCOVERY_OUTPUT required")
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	routes := []matrixRoute{{"anthropic-direct", "anthropic", "direct", "claude-haiku-5-5", "claude-haiku-5-5"}, {"openai-native", "openai", "vercel-native", "gpt-5.6-luna", "openai/gpt-5.6-luna"}, {"anthropic-native", "anthropic", "vercel-native", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"}, {"anthropic-messages", "anthropic", "vercel-anthropic", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"}, {"openai-direct", "openai", "direct", "gpt-5.6-luna", "gpt-5.6-luna"}, {"openai-responses", "openai", "vercel-openai", "gpt-5.6-luna", "openai/gpt-5.6-luna"}}
	for _, route := range routes {
		if pick := os.Getenv("ADK_DISCOVERY_ROUTE"); pick != "" && pick != route.Name {
			continue
		}
		t.Run(route.Name, func(t *testing.T) {
			if route.Name == "anthropic-direct" && os.Getenv("ANTHROPIC_API_KEY") == "" {
				t.Skip("ANTHROPIC_API_KEY required for direct Anthropic live testing")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 240*time.Second)
			defer cancel()
			cfg := adkmodels.ModelConfig{CanonicalModel: route.Model, RequestModel: route.Request, ToolDiscovery: tooldiscovery.Native, DefaultMaxOutputTokens: 1024, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelLow}, ToolSchemas: toolschema.Config{AllowUnsupported: true}}
			cfg.PromptCaching.OpenAI = adkmodels.OpenAIPromptCachingConfig{Mode: adkmodels.OpenAIPromptCacheImplicit, SystemInstruction: &adkmodels.OpenAICacheBreakpoint{}}
			cfg.PromptCaching.Anthropic = adkmodels.AnthropicPromptCachingConfig{Mode: adkmodels.AnthropicPromptCacheManual, SystemInstruction: &adkmodels.AnthropicCacheBreakpoint{}}
			if route.Path != "direct" {
				cfg.Vercel = &adkmodels.VercelConfig{Only: []string{route.Provider}, ZeroDataRetention: true, SystemInstructionCacheOptions: map[string]map[string]any{route.Provider: {"cacheControl": map[string]any{"type": "ephemeral"}}}}
				if route.Provider == "openai" {
					cfg.Vercel.SystemInstructionCacheOptions = map[string]map[string]any{"openai": {"promptCacheBreakpoint": map[string]any{"mode": "explicit"}}}
				}
			}
			capture := &strictLiveResponseCapture{base: http.DefaultTransport}
			tr := &matrixTransport{base: capture}
			inner, err := matrixConfiguredModel(ctx, route, cfg.ToolSchemas, tr, cfg, 110*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			recorder := &discoveryLiveRecorder{LLM: inner}
			type lookupArgs struct {
				ID string `json:"id"`
			}
			executed := map[string][]string{}
			tools := []tool.Tool{}
			for _, n := range []string{"lookup_probe", "audit_probe"} {
				name := n
				fn, err := functiontool.New(functiontool.Config{Name: name, Description: "Synthetic test recorder. Returns the supplied identifier unchanged."}, func(_ agent.Context, a lookupArgs) (map[string]any, error) {
					executed[name] = append(executed[name], a.ID)
					return map[string]any{"verified_id": a.ID}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				tools = append(tools, fn)
			}
			type searchArgs struct {
				Query string `json:"query"`
			}
			search, err := functiontool.New(functiontool.Config{Name: "search_tools", Description: "Discover available tools. Search for lookup_probe or audit_probe before calling one."}, func(c agent.Context, a searchArgs) (map[string]any, error) {
				if strings.Contains(a.Query, "missing") {
					return map[string]any{"status": "error", "loaded": []any{}, "message": "NO_MATCH_7821"}, nil
				}
				name := "lookup_probe"
				if strings.Contains(a.Query, "audit") {
					name = "audit_probe"
				}
				if err := c.State().Set("loaded_"+name, true); err != nil {
					return nil, err
				}
				return map[string]any{"status": "loaded", "loaded": []any{map[string]any{"name": name, "usage": "Use the exact identifier supplied by the user."}}, "feedback": "SYNTHETIC_SEARCH_FEEDBACK"}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			tools = append(tools, search)
			prefix := fmt.Sprintf("Synthetic cache test %d. No real data.\n", time.Now().UnixNano()) + strings.Repeat("The blue folder contains a routine sample with a numbered reference.\n", 850)
			a, err := llmagent.New(llmagent.Config{Name: "discovery_probe", Model: recorder, Tools: tools, Instruction: prefix + "\nFollow the user. Use search_tools to discover tools when asked. After a recorder returns, reply with its verified_id. Do not repeat successful calls.", BeforeModelCallbacks: []llmagent.BeforeModelCallback{func(c agent.Context, r *model.LLMRequest) (*model.LLMResponse, error) {
				// Reproduce the JSON persistence boundary used by the backend session store.
				data, err := json.Marshal(r.Contents)
				if err != nil {
					return nil, err
				}
				var history []*genai.Content
				if err = json.Unmarshal(data, &history); err != nil {
					return nil, err
				}
				r.Contents = history
				for _, name := range []string{"lookup_probe", "audit_probe"} {
					v, _ := c.State().Get("loaded_" + name)
					if v != true {
						delete(r.Tools, name)
					}
				}
				return nil, nil
			}}})
			if err != nil {
				t.Fatal(err)
			}
			sessions := session.InMemoryService()
			s, err := sessions.Create(ctx, &session.CreateRequest{AppName: "discovery-live", UserID: "synthetic"})
			if err != nil {
				t.Fatal(err)
			}
			run, err := runner.New(runner.Config{AppName: "discovery-live", Agent: a, SessionService: sessions})
			if err != nil {
				t.Fatal(err)
			}
			ctx = tooldiscovery.WithCatalog(ctx, tooldiscovery.Catalog{SearchTool: "search_tools", ResolveResult: func(o map[string]any) ([]string, error) {
				names := []string{}
				if a, ok := o["loaded"].([]any); ok {
					for _, v := range a {
						if m, ok := v.(map[string]any); ok {
							names = append(names, m["name"].(string))
						}
					}
				}
				return names, nil
			}})
			var runErr error
			finalText := ""
			streamingMode := agent.StreamingModeSSE
			if os.Getenv("ADK_DISCOVERY_NONSTREAM") == "1" {
				streamingMode = agent.StreamingModeNone
			}
			for _, prompt := range []string{"Reply READY only.", "Find lookup_probe and call it with id probe-123.", "Now find audit_probe and call it with id audit-456.", "Search again for lookup_probe, but do not execute it. Report its usage guidance.", "Use search_tools with query missing_probe. Tell me the exact error code from its response, without calling a recorder."} {
				for event, e := range run.Run(ctx, "synthetic", s.Session.ID(), genai.NewContentFromText(prompt, genai.RoleUser), agent.RunConfig{StreamingMode: streamingMode}) {
					if event != nil && event.Content != nil {
						for _, part := range event.Content.Parts {
							if part.Text != "" {
								finalText += part.Text
							}
						}
					}
					if e != nil {
						runErr = e
						break
					}
				}
				if runErr != nil {
					break
				}
			}
			evidence := map[string]any{"route": route, "rows": recorder.Rows, "executed": executed, "http_statuses": tr.statuses, "raw_calls": capture.calls, "final_text": finalText, "json_history_replay": true, "streaming_mode": streamingMode}
			if runErr != nil {
				evidence["error"] = runErr.Error()
			}
			data, _ := json.MarshalIndent(evidence, "", "  ")
			if err := os.WriteFile(filepath.Join(out, route.Name+".json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if runErr != nil {
				t.Fatal(runErr)
			}
			if !strings.Contains(finalText, "NO_MATCH_7821") {
				t.Fatal("empty search feedback did not reach model")
			}

			if fmt.Sprint(executed["lookup_probe"]) != "[probe-123]" || fmt.Sprint(executed["audit_probe"]) != "[audit-456]" {
				t.Fatalf("unexpected executed calls: %v", executed)
			}
			hits := 0
			for _, row := range recorder.Rows {
				if u, ok := row["usage"].(*genai.GenerateContentResponseUsageMetadata); ok && u != nil && u.CachedContentTokenCount > 0 {
					hits++
				}
			}
			if route.Provider == "openai" && len(recorder.Rows) > 0 {
				last, _ := recorder.Rows[len(recorder.Rows)-1]["usage"].(*genai.GenerateContentResponseUsageMetadata)
				if last == nil || last.CachedContentTokenCount == 0 {
					t.Fatal("empty-search continuation lost cache reuse")
				}
			}
			if hits < 2 {
				t.Fatalf("insufficient cache reuse evidence: hits=%d rows=%d", hits, len(recorder.Rows))
			}
		})
	}
}
