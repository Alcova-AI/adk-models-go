package structuredoutput

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"
	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	adkopenai "github.com/Alcova-AI/adk-models-go/openai"
	adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestExtractJSON(t *testing.T) {
	type answer struct {
		Key     string `json:"key"`
		Another int    `json:"another,omitempty"`
	}
	part := func(text, id, phase string) *genai.Part {
		p := &genai.Part{Text: text}
		converters.SetMessageMetadata(p, converters.MessageMetadata{ID: id, Phase: phase})
		return p
	}
	for _, tc := range []struct {
		name    string
		content *genai.Content
		want    answer
		fail    bool
	}{
		{"nil content", nil, answer{}, true},
		{"empty content", &genai.Content{}, answer{}, true},
		{"valid JSON", &genai.Content{Parts: []*genai.Part{{Text: `{"key":"value"}`}}}, answer{Key: "value"}, false},
		{"multiple fields", &genai.Content{Parts: []*genai.Part{{Text: `{"key":"test","another":42}`}}}, answer{Key: "test", Another: 42}, false},
		{"fragments", &genai.Content{Parts: []*genai.Part{{Text: `{"key":`}, {Text: `"fragmented","another":42}`}}}, answer{Key: "fragmented", Another: 42}, false},
		{"invalid JSON", &genai.Content{Parts: []*genai.Part{{Text: `not valid json`}}}, answer{}, true},
		{"fences are not stripped", &genai.Content{Parts: []*genai.Part{{Text: "```json\n{\"key\":\"fenced\"}\n```"}}}, answer{}, true},
		{"fresh value for each fallback", &genai.Content{Parts: []*genai.Part{{Text: `{"another":99,"key":123}`}, {Text: `{"key":"fresh"}`}}}, answer{Key: "fresh"}, false},
		{"thoughts skipped", &genai.Content{Parts: []*genai.Part{{Thought: true, Text: `hidden`}, {Text: `{"key":"visible"}`}}}, answer{Key: "visible"}, false},
		{"empty commentary before final", &genai.Content{Parts: []*genai.Part{part(`{"key":""}`, "c", "commentary"), part(`{"key":"complete"}`, "f", "final_answer")}}, answer{Key: "complete"}, false},
		{"PDF disclaimer before final", &genai.Content{Parts: []*genai.Part{part(`{"key":"Cannot access PDF"}`, "c", "commentary"), part(`{"key":"complete"}`, "f", "final_answer")}}, answer{Key: "complete"}, false},
		{"fragmented final", &genai.Content{Parts: []*genai.Part{part(`{"key":`, "f", "final_answer"), part(`"complete"}`, "f", "final_answer")}}, answer{Key: "complete"}, false},
		{"empty final blocks unphased draft", &genai.Content{Parts: []*genai.Part{{Text: `{"key":"draft"}`}, part("", "f", "final_answer")}}, answer{}, true},
		{"empty commentary blocks unphased fallback", &genai.Content{Parts: []*genai.Part{{Text: `{"key":"draft"}`}, part("", "c", "commentary")}}, answer{}, true},
		{"empty final only fails", &genai.Content{Parts: []*genai.Part{part("", "f", "final_answer")}}, answer{}, true},
		{"empty final fragment with shared ID", &genai.Content{Parts: []*genai.Part{part("", "f", "final_answer"), part(`{"key":"complete"}`, "f", "final_answer")}}, answer{Key: "complete"}, false},
		{"empty phased thought is ignored", &genai.Content{Parts: []*genai.Part{{Thought: true, PartMetadata: map[string]any{converters.MessageMetadataKey: map[string]any{"phase": "final_answer"}}}, {Text: `{"key":"complete"}`}}}, answer{Key: "complete"}, false},
		{"commentary only fails", &genai.Content{Parts: []*genai.Part{part(`{"key":"draft"}`, "c", "commentary")}}, answer{}, true},
		{"multiple finals fail", &genai.Content{Parts: []*genai.Part{part(`{"key":"first"}`, "f1", "final_answer"), part(`{"key":"second"}`, "f2", "final_answer")}}, answer{}, true},
		{"malformed final cannot fall back to draft", &genai.Content{Parts: []*genai.Part{part(`{"key":"draft"}`, "c", "commentary"), part(`{`, "f", "final_answer")}}, answer{}, true},
		{"wrong final type cannot fall back to draft", &genai.Content{Parts: []*genai.Part{part(`{"key":"draft"}`, "c", "commentary"), part(`{"key":123}`, "f", "final_answer")}}, answer{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.content)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ExtractJSON[answer](tc.content)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v want failure=%v", err, tc.fail)
			}
			if !tc.fail && (got == nil || !reflect.DeepEqual(*got, tc.want)) {
				t.Fatalf("got=%#v want=%#v", got, tc.want)
			}
			after, err := json.Marshal(tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("extraction mutated history")
			}
		})
	}
}

func TestTextCandidates(t *testing.T) {
	part := func(text, id, phase string) *genai.Part {
		p := &genai.Part{Text: text}
		converters.SetMessageMetadata(p, converters.MessageMetadata{ID: id, Phase: phase})
		return p
	}
	for _, tc := range []struct {
		name  string
		parts []*genai.Part
		want  []string
		fail  bool
	}{
		{"empty commentary before final", []*genai.Part{part(`{"section":""}`, "c", "commentary"), part(`{"section":"complete"}`, "f", "final_answer")}, []string{`{"section":"complete"}`}, false},
		{"PDF disclaimer before final", []*genai.Part{part(`{"section":"Cannot access PDF"}`, "c", "commentary"), part(`{"section":"complete"}`, "f", "final_answer")}, []string{`{"section":"complete"}`}, false},
		{"fragmented final", []*genai.Part{part(`{"section":`, "f", "final_answer"), part(`"complete"}`, "f", "final_answer")}, []string{`{"section":"complete"}`}, false},
		{"commentary only", []*genai.Part{part(`{"section":""}`, "c", "commentary")}, nil, true},
		{"multiple final messages", []*genai.Part{part(`{"section":"first"}`, "f1", "final_answer"), part(`{"section":"second"}`, "f2", "final_answer")}, nil, true},
		{"malformed final has no draft fallback", []*genai.Part{part(`{"section":"draft"}`, "c", "commentary"), part(`{`, "f", "final_answer")}, []string{`{`}, false},
		{"mixed phased and unphased", []*genai.Part{part(`draft`, "c", "commentary"), {Text: `{"section":"complete"}`}}, nil, true},
		{"unknown phase", []*genai.Part{part(`{}`, "x", "future_phase")}, nil, true},
		{"unphased fragments and legacy fallbacks", []*genai.Part{{Text: `{"section":`}, {Text: `"complete"}`}}, []string{`{"section":"complete"}`, `{"section":`, `"complete"}`}, false},
		{"unphased complete objects", []*genai.Part{{Text: `{"section":"first"}`}, {Text: `{"section":"second"}`}}, []string{`{"section":"first"}{"section":"second"}`, `{"section":"first"}`, `{"section":"second"}`}, false},
		{"ignore thoughts nil and empty parts", []*genai.Part{nil, {Thought: true, Text: `hidden`}, {}, {Text: `{"section":"complete"}`}}, []string{`{"section":"complete"}`}, false},
		{"ignore phased thought", []*genai.Part{{Thought: true, Text: `hidden`, PartMetadata: map[string]any{converters.MessageMetadataKey: map[string]any{"phase": "commentary"}}}, {Text: `{}`}}, []string{`{}`}, false},
		{"no text", []*genai.Part{nil, {}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := &genai.Content{Parts: tc.parts}
			before, err := json.Marshal(content)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the same representation that persisted session history reloads.
			var restored genai.Content
			if err := json.Unmarshal(before, &restored); err != nil {
				t.Fatal(err)
			}
			got, err := textCandidates(&restored)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v, want failure=%v", err, tc.fail)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("candidates=%#v, want %#v", got, tc.want)
			}
			after, err := json.Marshal(restored)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("selection mutated response history")
			}
		})
	}
	if _, err := textCandidates(nil); err == nil {
		t.Fatal("nil content accepted")
	}
}

// TestStructuredOutputLiveMatrix sends at most 16 synthetic requests, with no SDK
// retries and a per-request timeout. It tests real provider output through the
// public extractor, not whether a provider happens to emit commentary on demand.
// Run explicitly with ADK_STRUCTURED_LIVE=1 and all three provider credentials.
func TestStructuredOutputLiveMatrix(t *testing.T) {
	if os.Getenv("ADK_STRUCTURED_LIVE") != "1" {
		t.Skip("set ADK_STRUCTURED_LIVE=1 for synthetic provider requests")
	}
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AI_GATEWAY_API_KEY"} {
		if os.Getenv(key) == "" {
			t.Fatalf("required credential %s is missing", key)
		}
	}
	routes := []struct{ Name, Provider, Path, Model, Request string }{
		{"openai-direct-responses", "openai", "direct", "gpt-5.6-luna", "gpt-5.6-luna"},
		{"openai-direct-chat", "openai", "direct-chat", "gpt-5.6-luna", "gpt-5.6-luna"},
		{"openai-vercel-responses", "openai", "vercel-openai", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"openai-vercel-chat", "openai", "vercel-chat", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"openai-vercel-native", "openai", "vercel-native", "gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"anthropic-direct-messages", "anthropic", "direct", "claude-haiku-5-5", "claude-haiku-5-5"},
		{"anthropic-vercel-messages", "anthropic", "vercel-anthropic", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"},
		{"anthropic-vercel-native", "anthropic", "vercel-native", "claude-haiku-5-5", "anthropic/claude-haiku-5.5"},
	}
	for _, route := range routes {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", route.Name, stream), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
				defer cancel()
				tr := &jsonLiveTransport{base: http.DefaultTransport}
				mc := adkmodels.ModelConfig{CanonicalModel: route.Model, RequestModel: route.Request, DefaultMaxOutputTokens: 1024}
				if route.Provider == "openai" {
					mc.Reasoning.DefaultLevel = genai.ThinkingLevelMinimal
				}
				if route.Path == "vercel-native" || route.Path == "vercel-openai" || route.Path == "vercel-chat" || route.Path == "vercel-anthropic" {
					mc.Vercel = &adkmodels.VercelConfig{Only: []string{route.Provider}, ZeroDataRetention: true}
				}
				var llm model.LLM
				var err error
				client := &http.Client{Transport: tr, Timeout: 60 * time.Second}
				switch {
				case route.Path == "vercel-native":
					llm, err = adkvercel.NewModel(adkvercel.Config{APIKey: os.Getenv("AI_GATEWAY_API_KEY"), HTTPClient: client, Model: mc})
				case route.Provider == "anthropic":
					key, base := os.Getenv("ANTHROPIC_API_KEY"), "https://api.anthropic.com"
					if route.Path == "vercel-anthropic" {
						key, base = os.Getenv("AI_GATEWAY_API_KEY"), "https://ai-gateway.vercel.sh"
					}
					sdk := anthropic.NewClient(anthropicoption.WithAPIKey(key), anthropicoption.WithBaseURL(base), anthropicoption.WithHTTPClient(client), anthropicoption.WithMaxRetries(0))
					llm, err = adkanthropic.NewModel(adkanthropic.Config{Client: sdk, Model: mc})
				default:
					key, base := os.Getenv("OPENAI_API_KEY"), "https://api.openai.com/v1"
					if strings.HasPrefix(route.Path, "vercel-") {
						key, base = os.Getenv("AI_GATEWAY_API_KEY"), "https://ai-gateway.vercel.sh/v1"
					}
					sdk := openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(base), option.WithMaxRetries(0), option.WithHTTPClient(client))
					api := adkopenai.APIResponses
					if route.Path == "direct-chat" || route.Path == "vercel-chat" {
						api = adkopenai.APIChatCompletions
					}
					llm, err = adkopenai.NewModel(adkopenai.Config{Client: sdk, API: api, Model: mc})
				}
				if err != nil {
					t.Fatal(err)
				}
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Synthetic meeting: Jordan will send the review letter by 30 October 2026. The agreed decision is Cobalt Mango. Return only JSON with decision equal to Cobalt Mango and owner equal to Jordan.", genai.RoleUser)}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 1024, ResponseSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"decision": {Type: genai.TypeString}, "owner": {Type: genai.TypeString}}, Required: []string{"decision", "owner"}}}}
				var final *model.LLMResponse
				partials := 0
				for resp, err := range llm.GenerateContent(ctx, req, stream) {
					if err != nil {
						t.Fatal(err)
					}
					if resp != nil {
						if resp.Partial {
							partials++
						} else {
							final = resp
						}
					}
				}
				if final == nil || final.Content == nil || final.FinishReason != genai.FinishReasonStop {
					t.Fatalf("missing successful final: %+v", final)
				}
				before, err := json.Marshal(final.Content)
				if err != nil {
					t.Fatal(err)
				}
				got, err := ExtractJSON[struct {
					Decision string `json:"decision"`
					Owner    string `json:"owner"`
				}](final.Content)
				if err != nil {
					t.Fatal(err)
				}
				if got.Decision != "Cobalt Mango" || got.Owner != "Jordan" {
					t.Fatalf("wrong extracted values: %+v", got)
				}
				after, err := json.Marshal(final.Content)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("extraction changed response")
				}
				if stream && partials == 0 {
					t.Fatal("stream yielded no partial responses")
				}
				md, _ := adkmodels.MetadataFromResponse(final)
				t.Logf("route=%s model=%s stream=%t partials=%d statuses=%v response_id=%s resolved_provider=%s extracted=%+v", route.Name, route.Request, stream, partials, tr.statuses, md.ResponseID, md.ResolvedProvider, got)
				if mc.Vercel != nil && md.ResolvedProvider != "" && md.ResolvedProvider != route.Provider {
					t.Fatalf("wrong gateway provider %s", md.ResolvedProvider)
				}
			})
		}
	}
}

// jsonLiveTransport records HTTP outcomes without changing request or response bodies.
type jsonLiveTransport struct {
	base     http.RoundTripper
	statuses []int
}

func (t *jsonLiveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if response != nil {
		t.statuses = append(t.statuses, response.StatusCode)
	}
	return response, err
}

func TestExtractJSONFinalMessageIdentity(t *testing.T) {
	part := func(text, id string) *genai.Part {
		p := &genai.Part{Text: text}
		converters.SetMessageMetadata(p, converters.MessageMetadata{ID: id, Phase: converters.MessagePhaseFinalAnswer})
		return p
	}
	for _, tc := range []struct {
		name  string
		parts []*genai.Part
		want  int
		fail  bool
	}{
		{"single final without ID", []*genai.Part{part("1", "")}, 1, false},
		{"fragments with shared ID", []*genai.Part{part("1", "f"), part("2", "f")}, 12, false},
		{"both IDs missing", []*genai.Part{part("1", ""), part("2", "")}, 0, true},
		{"first ID missing", []*genai.Part{part("1", ""), part("2", "f")}, 0, true},
		{"last ID missing", []*genai.Part{part("1", "f"), part("2", "")}, 0, true},
		{"different IDs", []*genai.Part{part("1", "f1"), part("2", "f2")}, 0, true},
		{"empty first final without ID", []*genai.Part{part("", ""), part("1", "f")}, 0, true},
		{"empty last final without ID", []*genai.Part{part("1", "f"), part("", "")}, 0, true},
		{"empty final with different ID", []*genai.Part{part("1", "f1"), part("", "f2")}, 0, true},
		{"empty fragment with shared ID", []*genai.Part{part("1", "f"), part("", "f")}, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := &genai.Content{Parts: tc.parts}
			before, err := json.Marshal(content)
			if err != nil {
				t.Fatal(err)
			}
			var restored genai.Content
			if err := json.Unmarshal(before, &restored); err != nil {
				t.Fatal(err)
			}
			got, err := ExtractJSON[int](&restored)
			if (err != nil) != tc.fail {
				t.Fatalf("result=%v error=%v want failure=%t", got, err, tc.fail)
			}
			if !tc.fail && (got == nil || *got != tc.want) {
				t.Fatalf("result=%v want=%d", got, tc.want)
			}
			after, err := json.Marshal(restored)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("extraction mutated response")
			}
		})
	}
}
