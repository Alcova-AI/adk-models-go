// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type matrixRoundTrip func(*http.Request) (*http.Response, error)

func (f matrixRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMatrixProbeCannotSendNeutralSchema(t *testing.T) {
	for _, body := range []string{`{"tools":[{"name":"schema_probe","inputSchema":{"type":"object"}}]}`, `{"tools":[{"functionDeclarations":[{"name":"schema_probe","parametersJsonSchema":{"type":"object"}}]}]}`} {
		received := ""
		tr := &matrixTransport{probe: map[string]any{"type": "object", "required": []any{"replacement"}}, base: matrixRoundTrip(func(r *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(r.Body)
			received = string(b)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		})}
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "https://example.invalid", strings.NewReader(body))
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if !strings.Contains(received, "replacement") || len(tr.schemas) != 1 {
			t.Fatalf("probe not sent/captured: %s", received)
		}
		b, _ := json.Marshal(tr.schemas)
		if strings.Contains(string(b), "Authorization") {
			t.Fatal("captured headers")
		}
	}
	tr := &matrixTransport{probe: map[string]any{"type": "object"}, base: matrixRoundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("neutral request reached HTTP"); return nil, nil })}
	req, _ := http.NewRequest("POST", "https://example.invalid", strings.NewReader(`{"tools":[]}`))
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("missing substitution accepted")
	}
}
func TestMatrixCollectsErrorsNamesAndFinalCalls(t *testing.T) {
	var result matrixResult
	partial := &model.LLMResponse{Partial: true, ErrorCode: "broken", ErrorMessage: "stream failed", FinishReason: genai.FinishReasonMaxTokens, Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "partial", Args: map[string]any{"value": 1}}}}}}
	if len(matrixCalls(&result, partial)) != 0 {
		t.Fatal("partial counted as final")
	}
	final := &model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "wrong_tool", Args: map[string]any{"value": 1}}}}}}
	if len(matrixCalls(&result, final)) != 1 || len(result.ResponseErrors) != 1 || len(result.FinishReasons) != 1 || result.ToolNames[0] != "wrong_tool" {
		t.Fatalf("lost evidence: %+v", result)
	}
}
func TestMatrixFiltersRejectTypos(t *testing.T) {
	for _, s := range []string{"missing", "a,a", ",a"} {
		if matrixFilterValid(s, []string{"a", "b"}) == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if matrixFilterValid("a,b", []string{"a", "b"}) != nil {
		t.Fatal("valid selection rejected")
	}
}
