package adkmodels_test

import (
	"context"
	"encoding/json"
	"fmt"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
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
				if !strings.Contains(string(raw), "Root guidance") {
					t.Fatalf("schema lost: %s", raw)
				}
				tool := body["tools"].([]any)[0].(map[string]any)
				var schema any
				for _, key := range []string{"parameters", "input_schema", "inputSchema"} {
					if candidate, ok := tool[key]; ok {
						schema = candidate
					}
				}
				compiler := validator.NewCompiler()
				if err := compiler.AddResource("urn:wire", schema); err != nil {
					t.Fatal(err)
				}
				compiled, err := compiler.Compile("urn:wire")
				if err != nil {
					t.Fatal(err)
				}
				for _, colour := range []any{nil, "RED", "BLUE", ""} {
					valid := colour == nil || colour == "RED"
					if err := compiled.Validate(map[string]any{"colour": colour}); (err == nil) != valid {
						t.Fatalf("wire colour %v validity mismatch: %v", colour, err)
					}
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

func TestSchemaNumbersRemainNumbersOnWire(t *testing.T) {
	for _, adapter := range []string{"openai", "anthropic", "vercel"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", adapter, stream), func(t *testing.T) {
				var body map[string]any
				client := &http.Client{Transport: wireTransport(func(r *http.Request) (*http.Response, error) {
					d := json.NewDecoder(r.Body)
					d.UseNumber()
					if err := d.Decode(&body); err != nil {
						return nil, err
					}
					return wireResponse(r, adapter, stream), nil
				})}
				name := "gpt-5.6-luna"
				if adapter == "anthropic" {
					name = "claude-test"
				}
				llm, err := wireModel(adapter, client, adkmodels.ModelConfig{CanonicalModel: name, ToolSchemas: toolschema.Config{AllowUnsupported: true, Warn: func(context.Context, toolschema.Warning) {}}})
				if err != nil {
					t.Fatal(err)
				}
				raw := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":5,"enum":[9007199254740993]},"items":{"type":"array","items":{"type":"integer"},"minItems":1}},"required":["count","items"],"additionalProperties":false}`)
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("test", genai.RoleUser)}, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "example", ParametersJsonSchema: raw}}}}}}
				for _, err := range llm.GenerateContent(t.Context(), req, stream) {
					if err != nil {
						t.Fatal(err)
					}
				}
				key := map[string]string{"openai": "parameters", "anthropic": "input_schema", "vercel": "inputSchema"}[adapter]
				schema := body["tools"].([]any)[0].(map[string]any)[key].(map[string]any)
				props := schema["properties"].(map[string]any)
				if got := props["count"].(map[string]any)["enum"].([]any)[0]; got != json.Number("9007199254740993") {
					t.Fatalf("numeric enum changed: %#v", got)
				}
				if got := props["items"].(map[string]any)["minItems"]; got != json.Number("1") {
					t.Fatalf("cardinality changed: %#v", got)
				}
				if adapter != "anthropic" {
					if got := props["count"].(map[string]any)["minimum"]; got != json.Number("5") {
						t.Fatalf("numeric bound changed: %#v", got)
					}
				}
			})
		}
	}
}
