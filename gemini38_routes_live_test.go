// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
)

// Synthetic input types mirror the two rejected application tool shapes.
type routeTodoInput struct {
	Todos []routeTodo `json:"todos" jsonschema:"list of todo items to create or update"`
}
type routeTodo struct {
	ID      int    `json:"id,omitempty" jsonschema:"unique identifier for existing todo - omit for new items"`
	Content string `json:"content" jsonschema:"the content or description of the todo item"`
	Status  string `json:"status" jsonschema:"status: todo, in_progress, completed, or deleted"`
}
type routeStageInput struct {
	Files []routeStageFile `json:"files" jsonschema:"required,Files to copy into Agent Sandbox."`
}
type routeStageFile struct {
	Source        string              `json:"source,omitempty" jsonschema:"Exact versioned Filestore URI or an authorised Filestore path. Mutually exclusive with skill_resource."`
	SkillResource *routeSkillResource `json:"skill_resource,omitempty" jsonschema:"Caller-visible skill resource. Mutually exclusive with source."`
	Destination   string              `json:"destination" jsonschema:"required,Canonical Agent Sandbox-relative destination path, for example inputs/template.docx."`
}
type routeSkillResource struct {
	SkillID string `json:"skill_id" jsonschema:"required,Opaque frontmatter.metadata.skill_id returned by load_skill."`
	Path    string `json:"path" jsonschema:"required,Exact file path inside the skill."`
}

func routeDeclaration[T any](t *testing.T, name string) *genai.FunctionDeclaration {
	t.Helper()
	tool, err := functiontool.New(functiontool.Config{Name: name, Description: "Synthetic schema check; never executed."}, func(agent.Context, T) (map[string]any, error) { return nil, fmt.Errorf("must not execute") })
	if err != nil {
		t.Fatal(err)
	}
	return tool.(interface {
		Declaration() *genai.FunctionDeclaration
	}).Declaration()
}

// This records provider acceptance, not a document-quality score. It never runs tools.
func TestGemini38RoutesLive(t *testing.T) {
	if os.Getenv("GEMINI38_LIVE") != "1" {
		t.Skip("set GEMINI38_LIVE=1 for paid synthetic route checks")
	}
	route, dir := os.Getenv("GEMINI38_ROUTE"), os.Getenv("GEMINI38_OUTPUT")
	if dir == "" {
		t.Fatal("GEMINI38_OUTPUT required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("output directory must be empty", err)
	}
	cases := []struct {
		fd   *genai.FunctionDeclaration
		args string
	}{
		{routeDeclaration[routeTodoInput](t, "todo_write"), `{"todos":[{"content":"Check synthetic document","status":"todo"}]}`},
		{routeDeclaration[routeStageInput](t, "sandbox_stage_files"), `{"files":[{"source":"inputs/example.txt","destination":"inputs/example.txt"}]}`},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream-%v", tc.fd.Name, stream), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
				defer cancel()
				tr := &matrixTransport{base: http.DefaultTransport}
				cfg := toolschema.Config{AllowUnsupported: true, Warn: func(context.Context, toolschema.Warning) {}}
				var llm model.LLM
				var err error
				mc := adkmodels.ModelConfig{CanonicalModel: "gemini-3.8-flash", RequestModel: "google/gemini-3.8-flash", DefaultMaxOutputTokens: 4096, ToolSchemas: cfg, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelHigh}, Vercel: &adkmodels.VercelConfig{Only: []string{"vertex"}, ZeroDataRetention: true}}
				client := &http.Client{Transport: tr, Timeout: 85 * time.Second}
				switch route {
				case "vertex":
					llm, err = matrixModel(ctx, matrixRoute{"gemini38-vertex", "google", "vertex", "gemini-3.8-flash", "gemini-3.8-flash"}, cfg, tr)
				case "vercel-native":
					llm, err = adkvercel.NewModel(adkvercel.Config{APIKey: os.Getenv("AI_GATEWAY_API_KEY"), HTTPClient: client, Model: mc})
				case "vercel-openai":
					llm, err = adkopenai.NewModel(adkopenai.Config{Client: openai.NewClient(openaioption.WithAPIKey(os.Getenv("AI_GATEWAY_API_KEY")), openaioption.WithBaseURL("https://ai-gateway.vercel.sh/v1"), openaioption.WithHTTPClient(client), openaioption.WithMaxRetries(0)), Model: mc})
				case "vercel-anthropic":
					llm, err = adkanthropic.NewModel(adkanthropic.Config{Client: anthropic.NewClient(anthropicoption.WithAPIKey(os.Getenv("AI_GATEWAY_API_KEY")), anthropicoption.WithBaseURL("https://ai-gateway.vercel.sh"), anthropicoption.WithHTTPClient(client), anthropicoption.WithMaxRetries(0)), Model: mc})
				default:
					t.Fatalf("unknown route %q", route)
				}
				if err != nil {
					t.Fatal(err)
				}
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Call "+tc.fd.Name+" once with exactly these synthetic arguments: "+tc.args, genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 4096, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}, Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{tc.fd}}}}}
				result := matrixResult{Route: route, Model: "gemini-3.8-flash", Case: tc.fd.Name, Stream: stream, Started: time.Now().UTC().Format(time.RFC3339)}
				for resp, e := range llm.GenerateContent(ctx, req, stream) {
					if e != nil {
						result.CallError = e.Error()
						break
					}
					for _, call := range matrixCalls(&result, resp) {
						result.Arguments = append(result.Arguments, call.Args)
					}
				}
				result.Statuses = tr.statuses
				result.WireSchemas = tr.schemas
				var wanted map[string]any
				if err := json.Unmarshal([]byte(tc.args), &wanted); err != nil {
					t.Fatal(err)
				}
				for _, args := range result.Arguments {
					result.RequestedMatch = append(result.RequestedMatch, reflect.DeepEqual(args, wanted))
				}
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%s-%v.json", route, tc.fd.Name, stream)), raw, 0600); err != nil {
					t.Fatal(err)
				}
				t.Logf("route=%s http=%v calls=%d error=%s", route, result.Statuses, len(result.Arguments), result.CallError)
				if result.CallError != "" || len(result.ResponseErrors) != 0 || len(result.Arguments) != 1 || !result.RequestedMatch[0] || len(result.ToolNames) != 1 || result.ToolNames[0] != tc.fd.Name {
					t.Error("route did not return the requested tool and exact arguments; see captured result")
				}
			})
		}
	}
}
