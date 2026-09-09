package adkvercel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Alcova-AI/adk-models-go/internal/protocol"
	"google.golang.org/genai"
)

func TestEnterpriseWebSearchWireTool(t *testing.T) {
	tools, err := convertTools([]*genai.Tool{{EnterpriseWebSearch: &genai.EnterpriseWebSearch{}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[{"type":"provider","name":"enterprise_web_search","id":"google.enterprise_web_search","args":{}}]` {
		t.Fatalf("wire tool: %s", data)
	}
	for _, search := range []*genai.EnterpriseWebSearch{{ExcludeDomains: []string{"example.com"}}, {BlockingConfidence: genai.PhishBlockThreshold("BLOCK_LOW_AND_ABOVE")}} {
		if _, err := convertTools([]*genai.Tool{{EnterpriseWebSearch: search}}); err == nil {
			t.Fatal("silently dropped search configuration")
		}
	}
	if _, err := convertTools([]*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}); err == nil {
		t.Fatal("ordinary Google Search accepted")
	}
}

func TestGroundingBothResponseModes(t *testing.T) {
	metadata := map[string]any{"vertex": map[string]any{"groundingMetadata": map[string]any{
		"groundingChunks":   []any{map[string]any{"web": map[string]any{"uri": "https://example.com/original", "title": "Original"}}},
		"groundingSupports": []any{map[string]any{"groundingChunkIndices": []int{0}, "segment": map[string]any{"text": "answer", "endIndex": 6}}},
		"webSearchQueries":  []string{"query"},
	}}}
	source := protocol.OutputPart{Type: "source", SourceType: "url", URL: "https://example.com/fallback", Title: "Fallback"}
	for _, withMetadata := range []bool{false, true} {
		provider := metadata
		if !withMetadata {
			provider = nil
		}
		want := "https://example.com/fallback"
		if withMetadata {
			want = "https://example.com/original"
		}
		response, err := convertGenerateResult(protocol.GenerateResult{Content: []protocol.OutputPart{source, {Type: "text", Text: "answer"}}, ProviderMetadata: provider}, false)
		if err != nil {
			t.Fatal(err)
		}
		state := newStreamState()
		_, _, err = state.convert(protocol.StreamPart{Type: "source", SourceType: source.SourceType, URL: source.URL, Title: source.Title}, false)
		if err != nil {
			t.Fatal(err)
		}
		streamed, terminal, err := state.convert(protocol.StreamPart{Type: "finish", ProviderMetadata: provider}, false)
		if err != nil || !terminal {
			t.Fatalf("finish: %v, terminal %v", err, terminal)
		}
		for _, grounding := range []*genai.GroundingMetadata{response.GroundingMetadata, streamed.GroundingMetadata} {
			if grounding == nil || len(grounding.GroundingChunks) != 1 || grounding.GroundingChunks[0].Web.URI != want {
				t.Fatalf("grounding: %#v", grounding)
			}
			if withMetadata && (len(grounding.GroundingSupports) != 1 || len(grounding.WebSearchQueries) != 1) {
				t.Fatal("grounding evidence dropped")
			}
		}
	}
	_, err := groundingMetadata(map[string]any{"vertex": map[string]any{"groundingMetadata": "malformed"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "grounding metadata") {
		t.Fatalf("malformed metadata error: %v", err)
	}
}

func TestEmptyFunctionSchemaRemainsOnWire(t *testing.T) {
	for _, declaration := range []*genai.FunctionDeclaration{
		{Name: "empty", ParametersJsonSchema: map[string]any{}},
		{Name: "empty", Parameters: &genai.Schema{}},
	} {
		tools, err := convertTools([]*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{declaration}}})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(tools)
		if err != nil {
			t.Fatal(err)
		}
		var wire []map[string]json.RawMessage
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		if string(wire[0]["inputSchema"]) != "{}" {
			t.Fatalf("missing empty function schema: %s", data)
		}
	}
}
