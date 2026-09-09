package adkmodels_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"github.com/Alcova-AI/adk-models-go/toolschema"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestToolSchemaWireConversion(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "vercel"} {
		for _, stream := range []bool{false, true} {
			t.Run(adapter+map[bool]string{false: "/sync", true: "/stream"}[stream], func(t *testing.T) {
				var body map[string]any
				client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						return nil, err
					}
					return wireResponse(r, adapter, stream), nil
				})}
				name, namespace := "gpt-5.6-luna", "openai"
				if adapter == "anthropic" {
					name, namespace = "claude-test", "anthropic"
				}
				cfg := adkmodels.ModelConfig{CanonicalModel: name, RequestModel: namespace + "/" + name, Vercel: &adkmodels.VercelConfig{}, ToolSchemas: toolschema.Config{AllowUnsupported: true, Warn: func(context.Context, toolschema.Warning) {}}}
				llm, err := wireModel(adapter, client, cfg)
				if err != nil {
					t.Fatal(err)
				}
				nullable := true
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("test", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "example", Parameters: &genai.Schema{Type: genai.TypeObject, Description: "Root guidance", Properties: map[string]*genai.Schema{"colour": {Type: genai.TypeString, Nullable: &nullable, Enum: []string{"RED"}}}}}}}}}}
				before, _ := json.Marshal(req)
				for _, err := range llm.GenerateContent(t.Context(), req, stream) {
					if err != nil {
						t.Fatal(err)
					}
				}
				after, _ := json.Marshal(req)
				if string(before) != string(after) {
					t.Fatal("caller request mutated")
				}
				if body == nil {
					t.Fatal("no request")
				}
				raw, _ := json.Marshal(body)
				if strings.Contains(string(raw), `"nullable"`) {
					t.Fatalf("nullable leaked: %s", raw)
				}
				if !strings.Contains(string(raw), `"anyOf"`) || !strings.Contains(string(raw), "Root guidance") {
					t.Fatalf("schema lost: %s", raw)
				}
				if !strings.Contains(string(raw), `"strict":false`) {
					t.Fatalf("best-effort mode not explicit: %s", raw)
				}
			})
		}
	}
}
func TestMalformedSchemaNeverReachesProvider(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "vercel"} {
		t.Run(adapter, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) { calls++; return wireResponse(r, adapter, false), nil })}
			name, namespace := "gpt-5.6-luna", "openai"
			if adapter == "anthropic" {
				name, namespace = "claude-test", "anthropic"
			}
			cfg := adkmodels.ModelConfig{CanonicalModel: name, RequestModel: namespace + "/" + name, Vercel: &adkmodels.VercelConfig{}, ToolSchemas: toolschema.Config{AllowUnsupported: true}}
			llm, err := wireModel(adapter, client, cfg)
			if err != nil {
				t.Fatal(err)
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("test", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "test", ParametersJsonSchema: map[string]any{"type": "object", "nullable": true}}}}}}}
			failed := false
			for _, err := range llm.GenerateContent(t.Context(), req, false) {
				if err != nil {
					failed = true
				}
			}
			if !failed || calls != 0 {
				t.Fatalf("failed=%v requests=%d", failed, calls)
			}
		})
	}
}

func TestDefaultToolSchemaPolicyOnWire(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "vercel"} {
		for _, closed := range []bool{false, true} {
			t.Run(adapter+map[bool]string{true: "/compatible", false: "/needs-opt-in"}[closed], func(t *testing.T) {
				var body map[string]any
				calls := 0
				client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						return nil, err
					}
					return wireResponse(r, adapter, false), nil
				})}
				name, namespace := "gpt-5.6-luna", "openai"
				if adapter == "anthropic" {
					name, namespace = "claude-test", "anthropic"
				}
				llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: name, RequestModel: namespace + "/" + name, Vercel: &adkmodels.VercelConfig{}})
				if err != nil {
					t.Fatal(err)
				}
				schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}, "required": []string{"x"}}
				if closed {
					schema["additionalProperties"] = false
				}
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("test", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "test", ParametersJsonSchema: schema}}}}}}
				var callErr error
				for _, err := range llm.GenerateContent(t.Context(), req, false) {
					if err != nil {
						callErr = err
					}
				}
				if !closed {
					if callErr == nil || calls != 0 {
						t.Fatalf("error=%v calls=%d", callErr, calls)
					}
					return
				}
				if callErr != nil {
					t.Fatal(callErr)
				}
				raw, _ := json.Marshal(body)
				if calls != 1 || !strings.Contains(string(raw), `"strict":true`) {
					t.Fatalf("did not enable strict: %s", raw)
				}
			})
		}
	}
}
