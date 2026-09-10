// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
)

type matrixRoute struct{ Name, Provider, Path, Model, Request string }

func matrixRoutes() []matrixRoute {
	return []matrixRoute{
		{"luna-direct", "openai", "direct", "gpt-5.6-luna", "gpt-5.6-luna"},
		{"luna-native", "openai", "vercel-native", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"luna-responses", "openai", "vercel-openai", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"haiku-vertex", "anthropic", "vertex", "claude-haiku-4-5", "claude-haiku-4-5"},
		{"haiku-native", "anthropic", "vercel-native", "claude-haiku-4-5", "anthropic/claude-haiku-4.5"},
		{"haiku-messages", "anthropic", "vercel-anthropic", "claude-haiku-4-5", "anthropic/claude-haiku-4.5"},
		{"gemini-vertex", "google", "vertex", "gemini-3.1-flash-lite", "gemini-3.1-flash-lite"},
		{"gemini-native", "google", "vercel-native", "gemini-3.1-flash-lite", "google/gemini-3.1-flash-lite"},
		{"gemini-responses", "google", "vercel-openai", "gemini-3.1-flash-lite", "google/gemini-3.1-flash-lite"},
		{"gemini-messages", "google", "vercel-anthropic", "gemini-3.1-flash-lite", "google/gemini-3.1-flash-lite"},
	}
}

// Capture only schema-related fields, never authentication headers or response
// reasoning. Probe mode replaces a neutral schema at the HTTP boundary so local
// catalogue rejection cannot be mistaken for provider rejection.
type matrixTransport struct {
	base     http.RoundTripper
	probe    map[string]any
	mu       sync.Mutex
	schemas  []any
	statuses []int
}

func (m *matrixTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		raw, e := io.ReadAll(req.Body)
		if e != nil {
			return nil, e
		}
		_ = req.Body.Close()
		replaced := 0
		var obj map[string]any
		if json.Unmarshal(raw, &obj) == nil {
			for _, tool := range asArray(obj["tools"]) {
				x, _ := tool.(map[string]any)
				if x == nil {
					continue
				}
				nodes := []map[string]any{x}
				for _, fd := range asArray(x["functionDeclarations"]) {
					if n, ok := fd.(map[string]any); ok {
						nodes = append(nodes, n)
					}
				}
				for _, n := range nodes {
					for _, key := range []string{"parameters", "parametersJsonSchema", "input_schema", "inputSchema"} {
						if _, ok := n[key]; !ok {
							continue
						}
						if m.probe != nil {
							n[key] = m.probe
							replaced++
						}
						snapshot := map[string]any{"field": key, "schema": n[key], "strict": n["strict"]}
						m.mu.Lock()
						m.schemas = append(m.schemas, snapshot)
						m.mu.Unlock()
					}
				}
			}
			raw, e = json.Marshal(obj)
			if e != nil {
				return nil, e
			}
		}
		if m.probe != nil && replaced != 1 {
			return nil, fmt.Errorf("probe must replace exactly one schema, replaced %d", replaced)
		}
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
	}
	resp, err := m.base.RoundTrip(req)
	if resp != nil {
		m.mu.Lock()
		m.statuses = append(m.statuses, resp.StatusCode)
		m.mu.Unlock()
	}
	return resp, err
}
func asArray(v any) []any { x, _ := v.([]any); return x }
func matrixModel(ctx context.Context, r matrixRoute, cfg toolschema.Config, tr *matrixTransport) (model.LLM, error) {
	mc := adkmodels.ModelConfig{CanonicalModel: r.Model, RequestModel: r.Request, DefaultMaxOutputTokens: 512, ToolSchemas: cfg, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelMinimal}}
	if r.Provider == "anthropic" {
		mc.Reasoning.DefaultLevel = ""
	}
	client := &http.Client{Transport: tr, Timeout: 50 * time.Second}
	if strings.HasPrefix(r.Path, "vercel") {
		mc.Vercel = &adkmodels.VercelConfig{}
	}
	switch r.Path {
	case "vercel-native":
		return adkvercel.NewModel(adkvercel.Config{APIKey: os.Getenv("AI_GATEWAY_API_KEY"), HTTPClient: client, Model: mc})
	case "vercel-openai":
		return adkopenai.NewModel(adkopenai.Config{Client: openai.NewClient(openaioption.WithAPIKey(os.Getenv("AI_GATEWAY_API_KEY")), openaioption.WithBaseURL("https://ai-gateway.vercel.sh/v1"), openaioption.WithHTTPClient(client), openaioption.WithMaxRetries(0)), Model: mc})
	case "vercel-anthropic":
		return adkanthropic.NewModel(adkanthropic.Config{Client: anthropic.NewClient(anthropicoption.WithAPIKey(os.Getenv("AI_GATEWAY_API_KEY")), anthropicoption.WithBaseURL("https://ai-gateway.vercel.sh"), anthropicoption.WithHTTPClient(client), anthropicoption.WithMaxRetries(0)), Model: mc})
	case "direct":
		return adkopenai.NewModel(adkopenai.Config{Client: openai.NewClient(openaioption.WithAPIKey(os.Getenv("OPENAI_API_KEY")), openaioption.WithHTTPClient(client), openaioption.WithMaxRetries(0)), Model: mc})
	case "vertex":
		region := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if region == "" {
			region = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if region == "" {
			region = "us-east5"
		}
		creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, err
		}
		tr.base = &oauth2.Transport{Source: creds.TokenSource, Base: http.DefaultTransport}
		if r.Provider == "anthropic" {
			return adkanthropic.NewModel(adkanthropic.Config{Client: anthropic.NewClient(vertex.WithCredentials(ctx, region, os.Getenv("GOOGLE_CLOUD_PROJECT"), creds), anthropicoption.WithHTTPClient(client), anthropicoption.WithMaxRetries(0)), Model: mc})
		}
		llm, err := gemini.NewModel(ctx, r.Model, &genai.ClientConfig{Backend: genai.BackendVertexAI, Project: os.Getenv("GOOGLE_CLOUD_PROJECT"), Location: region, HTTPClient: client})
		if err != nil {
			return nil, err
		}
		return toolschema.WrapGemini(llm, cfg, "vertex"), nil
	}
	return nil, fmt.Errorf("unknown route %s", r.Path)
}

type matrixResult struct {
	Route, Model, Case, Mode, Prompt, Started string
	Stream                                    bool
	Original                                  map[string]any
	Prepared                                  map[string]toolschema.Prepared `json:",omitempty"`
	Warnings                                  []toolschema.Warning           `json:",omitempty"`
	CollectionError                           string                         `json:",omitempty"`
	LocalError, CallError                     string                         `json:",omitempty"`
	Statuses                                  []int
	WireSchemas                               []any
	Arguments                                 []map[string]any
	OriginalValid, PreparedValid              []bool
	RequestedMatch                            []bool
	ToolNames                                 []string
	ResponseErrors                            []string
	FinishReasons                             []string
	DurationMS                                int64
}

// This is an observational matrix: rejection and nonconformance are results,
// not grounds to skip cases. Setup, oracle and output failures fail the test.
// A passing run means collection completed, never that all providers conformed.
func TestSchemaMatrixLive(t *testing.T) {
	if os.Getenv("ADK_SCHEMA_LIVE") != "1" {
		t.Skip("set ADK_SCHEMA_LIVE=1 to make paid synthetic provider calls")
	}
	dir := os.Getenv("ADK_SCHEMA_OUTPUT")
	if dir == "" {
		t.Fatal("ADK_SCHEMA_OUTPUT is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// A fresh directory prevents a partial rerun from silently mixing snapshots.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("output directory must be empty")
	}
	routeNames := []string{}
	for _, r := range matrixRoutes() {
		routeNames = append(routeNames, r.Name)
	}
	caseNames := []string{}
	for _, c := range matrixCases(t) {
		caseNames = append(caseNames, c.Name)
	}
	for name, known := range map[string][]string{"ADK_SCHEMA_ROUTES": routeNames, "ADK_SCHEMA_CASES": caseNames, "ADK_SCHEMA_MODES": {"default", "fallback", "probe"}} {
		if e := matrixFilterValid(os.Getenv(name), known); e != nil {
			t.Fatalf("%s: %v", name, e)
		}
	}
	hashes := map[string]string{}
	for _, name := range []string{"schema_matrix_live_test.go", "schema_matrix_cases_test.go", "toolschema/schema.go", "toolschema/support.go", "toolschema/native.go", "toolschema/optional.go", "openai/openai.go", "anthropic/anthropic.go", "vercel/request.go", "vercel/response.go", "vercel/stream.go", "config.go", "openai/stream.go", "go.mod", "testdata/schema-matrix/legacy-list-clients.genai.json"} {
		b, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	planned := []string{}
	t.Cleanup(func() {
		manifest := map[string]any{"source_sha256": hashes, "planned": planned, "stream": os.Getenv("ADK_SCHEMA_STREAM") == "1", "format_version": 2, "collection_failed": t.Failed(), "note": "Collection only. Conforming samples do not prove guaranteed enforcement."}
		b, e := json.MarshalIndent(manifest, "", "  ")
		if e != nil {
			t.Error(e)
			return
		}
		if e = os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); e != nil {
			t.Error(e)
		}
	})
	for _, r := range matrixRoutes() {
		if filter := os.Getenv("ADK_SCHEMA_ROUTES"); filter != "" && !strings.Contains(","+filter+",", ","+r.Name+",") {
			continue
		}
		for _, tc := range matrixCases(t) {
			if filter := os.Getenv("ADK_SCHEMA_CASES"); filter != "" && !strings.Contains(","+filter+",", ","+tc.Name+",") {
				continue
			}
			for _, mode := range []string{"default", "fallback", "probe"} {
				if filter := os.Getenv("ADK_SCHEMA_MODES"); filter != "" && !strings.Contains(","+filter+",", ","+mode+",") {
					continue
				}
				// Fallback adds value only when the default policy rejects this schema.
				if mode == "fallback" {
					_, err := toolschema.New(toolschema.Config{}, toolschema.Target{Provider: r.Provider, Route: r.Path}).Prepare(t.Context(), []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{matrixDeclaration(tc)}}})
					if err == nil {
						continue
					}
				}
				for _, prompt := range []string{"valid", "conflict"} {
					planned = append(planned, r.Name+"/"+tc.Name+"/"+mode+"/"+prompt)
					t.Run(r.Name+"/"+tc.Name+"/"+mode+"/"+prompt, func(t *testing.T) {
						t.Parallel()
						result := matrixResult{Route: r.Name, Model: r.Request, Case: tc.Name, Mode: mode, Prompt: prompt, Original: tc.Schema, Started: time.Now().UTC().Format(time.RFC3339), Stream: os.Getenv("ADK_SCHEMA_STREAM") == "1"}
						start := time.Now()
						tr := &matrixTransport{base: http.DefaultTransport}
						defer func() {
							result.DurationMS = time.Since(start).Milliseconds()
							tr.mu.Lock()
							result.Statuses = tr.statuses
							result.WireSchemas = tr.schemas
							if mode == "probe" && len(tr.schemas) == 0 {
								t.Error("provider probe did not capture a schema")
							}
							tr.mu.Unlock()
							if t.Failed() && result.CollectionError == "" {
								result.CollectionError = "test collection failed; see test output"
							}
							b, e := json.MarshalIndent(result, "", "  ")
							if e != nil {
								t.Error(e)
								return
							}
							name := r.Name + "-" + tc.Name + "-" + mode + "-" + prompt + ".json"
							if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
								t.Error(e)
							}
							t.Logf("http=%v calls=%d conforming=%v local_error=%t remote_error=%t", result.Statuses, len(result.Arguments), result.OriginalValid, result.LocalError != "", result.CallError != "")
						}()
						var wm sync.Mutex
						cfg := toolschema.Config{AllowUnsupported: mode == "fallback", Warn: func(_ context.Context, w toolschema.Warning) {
							wm.Lock()
							defer wm.Unlock()
							result.Warnings = append(result.Warnings, w)
						}}
						fd := matrixDeclaration(tc)
						prepared, e := toolschema.New(cfg, toolschema.Target{Provider: r.Provider, Route: r.Path}).Prepare(t.Context(), []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{fd}}})
						result.Prepared = prepared
						if e != nil {
							result.LocalError = e.Error()
							if mode != "probe" {
								return
							}
						}
						if mode == "probe" {
							tr.probe = tc.Schema
							fd.Parameters = nil
							fd.ParametersJsonSchema = map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}, "additionalProperties": false}
							cfg.AllowUnsupported = r.Provider == "google"
						}
						ctx, cancel := context.WithTimeout(t.Context(), 55*time.Second)
						defer cancel()
						llm, e := matrixModel(ctx, r, cfg, tr)
						if e != nil {
							result.CollectionError = "model setup: " + e.Error()
							t.Fatal(result.CollectionError)
						}
						desired := tc.Valid
						if prompt == "conflict" {
							desired = tc.Conflict
						}
						req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Call schema_probe exactly once with precisely these arguments: "+desired+". Do not explain. This is a synthetic test.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 512, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}, Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{fd}}}, ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}}}}
						// Forced Claude tool use excludes thinking; leave its controls unset.
						if r.Provider == "anthropic" {
							req.Config.ThinkingConfig = nil
						}
						original, e := matrixValidator(tc.Schema)
						if e != nil {
							t.Fatal(e)
						}
						for resp, e := range llm.GenerateContent(ctx, req, result.Stream) {
							if e != nil {
								result.CallError = e.Error()
								break
							}
							for _, call := range matrixCalls(&result, resp) {
								args := call.Args
								result.Arguments = append(result.Arguments, args)
								var wanted, actual any
								_ = json.Unmarshal([]byte(desired), &wanted)
								b, _ := json.Marshal(args)
								_ = json.Unmarshal(b, &actual)
								result.RequestedMatch = append(result.RequestedMatch, reflect.DeepEqual(wanted, actual))
								result.OriginalValid = append(result.OriginalValid, original.Validate(args) == nil)
								if s, ok := prepared[fd.Name]; ok {
									v, e := matrixValidator(s.Schema)
									if e != nil {
										t.Fatal(e)
									}
									result.PreparedValid = append(result.PreparedValid, v.Validate(args) == nil)
								}
							}
						}
					})
				}
			}
		}
	}
}

func matrixFilterValid(filter string, known []string) error {
	if filter == "" {
		return nil
	}
	set := map[string]bool{}
	for _, s := range known {
		set[s] = true
	}
	seen := map[string]bool{}
	for _, s := range strings.Split(filter, ",") {
		if !set[s] || seen[s] {
			return fmt.Errorf("unknown or duplicate selection %q", s)
		}
		seen[s] = true
	}
	return nil
}

func matrixCalls(result *matrixResult, resp *model.LLMResponse) []*genai.FunctionCall {
	if resp == nil {
		return nil
	}
	if resp.ErrorCode != "" || resp.ErrorMessage != "" {
		result.ResponseErrors = append(result.ResponseErrors, resp.ErrorCode+": "+resp.ErrorMessage)
	}
	if resp.FinishReason != "" {
		result.FinishReasons = append(result.FinishReasons, string(resp.FinishReason))
	}
	if resp.Partial || resp.Content == nil {
		return nil
	}
	calls := []*genai.FunctionCall{}
	for _, part := range resp.Content.Parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}
		result.ToolNames = append(result.ToolNames, part.FunctionCall.Name)
		calls = append(calls, part.FunctionCall)
	}
	return calls
}
