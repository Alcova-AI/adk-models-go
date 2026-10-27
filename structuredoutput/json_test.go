package structuredoutput

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// TestPhaseHistoryLiveReplay exercises real three-call tool conversations. The
// model must emit its own commentary/final phases; the test never injects them.
// ADK_PHASE_REPLAY_EMPTY_COMMENTARY=1 clears generated commentary text only in
// the saved history fixture, to prove real endpoints accept empty phased replay.
func TestPhaseHistoryLiveReplay(t *testing.T) {
	if os.Getenv("ADK_PHASE_REPLAY_LIVE") != "1" {
		t.Skip("set ADK_PHASE_REPLAY_LIVE=1 for live phase replay")
	}
	for _, key := range []string{"OPENAI_API_KEY", "AI_GATEWAY_API_KEY"} {
		if os.Getenv(key) == "" {
			t.Fatalf("missing %s", key)
		}
	}
	emptyCommentary := os.Getenv("ADK_PHASE_REPLAY_EMPTY_COMMENTARY") == "1"
	for _, route := range []string{"direct-responses", "vercel-responses", "vercel-native"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", route, stream), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
				defer cancel()
				capture := &phaseReplayCapture{base: http.DefaultTransport}
				client := &http.Client{Transport: capture, Timeout: 90 * time.Second}
				mc := adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "gpt-5.6-luna", DefaultMaxOutputTokens: 4096, Reasoning: adkmodels.ReasoningConfig{DefaultLevel: genai.ThinkingLevelLow}}
				if route != "direct-responses" {
					mc.RequestModel = "openai/gpt-5.6-luna"
					mc.Vercel = &adkmodels.VercelConfig{Only: []string{"openai"}, ZeroDataRetention: true}
				}
				var llm model.LLM
				var err error
				if route == "vercel-native" {
					llm, err = adkvercel.NewModel(adkvercel.Config{APIKey: os.Getenv("AI_GATEWAY_API_KEY"), HTTPClient: client, Model: mc})
				} else {
					key, base := os.Getenv("OPENAI_API_KEY"), "https://api.openai.com/v1"
					if route == "vercel-responses" {
						key, base = os.Getenv("AI_GATEWAY_API_KEY"), "https://ai-gateway.vercel.sh/v1"
					}
					sdk := openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(base), option.WithMaxRetries(0), option.WithHTTPClient(client))
					llm, err = adkopenai.NewModel(adkopenai.Config{Client: sdk, API: adkopenai.APIResponses, Model: mc})
				}
				if err != nil {
					t.Fatal(err)
				}
				config := &genai.GenerateContentConfig{MaxOutputTokens: 4096, Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup_token", Description: "Read the secret synthetic test token. Call exactly once before answering.", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}}}}}}, ResponseSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"token": {Type: genai.TypeString}, "status": {Type: genai.TypeString}}, Required: []string{"token", "status"}}}
				history := []*genai.Content{genai.NewContentFromText("This is a synthetic integration test. Retrieve the token using lookup_token. Before calling the tool, send the user one short progress update in a separate commentary message, using JSON with token empty and status checking. Then call lookup_token exactly once. After receiving its result, return a final JSON answer with the exact token and status complete. Do not invent the token. Do not skip the progress update.", genai.RoleUser)}
				marker := fmt.Sprintf("Cobalt-Replay-%s-%t", route, stream)
				phases := []string{}
				partials := []int{}
				for turn := 0; turn < 3; turn++ {
					requestIndex := len(capture.calls)
					var final *model.LLMResponse
					chunks := 0
					for resp, e := range llm.GenerateContent(ctx, &model.LLMRequest{Contents: history, Config: config}, stream) {
						if e != nil {
							t.Fatal(e)
						}
						if resp == nil {
							continue
						}
						if resp.Partial {
							chunks++
						} else {
							final = resp
						}
					}
					if final == nil || final.Content == nil {
						t.Fatalf("turn %d has no complete response", turn)
					}
					if len(capture.calls) != requestIndex+1 || capture.calls[requestIndex].Status != 200 {
						t.Fatalf("unexpected requests/statuses: %#v", capture.calls)
					}
					partials = append(partials, chunks)
					if stream && chunks == 0 {
						t.Fatalf("turn %d had no streaming partials", turn)
					}
					if turn > 0 {
						assertPhaseReplayWire(t, capture.calls[requestIndex].Request, history, route == "vercel-native")
					}
					// Exercise the same JSON persistence boundary as saved session history.
					raw, e := json.Marshal(final.Content)
					if e != nil {
						t.Fatal(e)
					}
					restored := new(genai.Content)
					if e = json.Unmarshal(raw, restored); e != nil {
						t.Fatal(e)
					}
					if turn == 0 && emptyCommentary {
						cleared := 0
						for _, part := range restored.Parts {
							m, _ := converters.MessageMetadataFromPart(part)
							if m.Phase == "commentary" {
								part.Text = ""
								cleared++
							}
						}
						if cleared == 0 {
							t.Fatal("no generated commentary to use for the empty replay fixture")
						}
					}
					calls := []*genai.FunctionCall{}
					for _, part := range restored.Parts {
						if m, ok := converters.MessageMetadataFromPart(part); ok && m.Phase != "" {
							phases = append(phases, m.Phase)
						}
						if part.FunctionCall != nil {
							calls = append(calls, part.FunctionCall)
						}
					}
					history = append(history, restored)
					if turn == 0 {
						if len(calls) != 1 || calls[0].Name != "lookup_token" {
							t.Fatalf("first turn must call lookup_token once: %s", raw)
						}
						found := false
						for _, p := range restored.Parts {
							m, _ := converters.MessageMetadataFromPart(p)
							if m.Phase == "commentary" && m.ID != "" {
								found = true
							}
						}
						if !found {
							t.Fatalf("model did not produce commentary metadata; cannot prove commentary replay: %s", raw)
						}
						history = append(history, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: calls[0].Name, ID: calls[0].ID, Response: map[string]any{"token": marker}}}}})
					} else {
						if len(calls) != 0 {
							t.Fatalf("unexpected extra tool call on turn %d", turn)
						}
						got, e := ExtractJSON[struct {
							Token  string `json:"token"`
							Status string `json:"status"`
						}](restored)
						if e != nil || got == nil || got.Token != marker || got.Status != "complete" {
							t.Fatalf("turn %d final=%s extracted=%+v error=%v", turn, raw, got, e)
						}
						found := false
						for _, p := range restored.Parts {
							m, _ := converters.MessageMetadataFromPart(p)
							if m.Phase == "final_answer" && m.ID != "" {
								found = true
							}
						}
						if !found {
							t.Fatalf("turn %d missing final_answer phase: %s", turn, raw)
						}
						if turn == 1 {
							history = append(history, genai.NewContentFromText("Without using any tools, repeat the exact token you just retrieved as JSON with status complete.", genai.RoleUser))
						}
					}
				}
				if dir := os.Getenv("ADK_PHASE_REPLAY_PROOF_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					data, err := json.MarshalIndent(map[string]any{"route": route, "stream": stream, "model": mc.RequestModel, "reasoning": "LOW", "empty_commentary_fixture": emptyCommentary, "phases": phases, "partials": partials, "calls": capture.calls, "history": history}, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(fmt.Sprintf("%s/%s-%t.json", dir, route, stream), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Logf("route=%s stream=%t real_calls=%d tool_results=1 replay_assertions=2 empty_commentary_fixture=%t phases=%v partials=%v token_verified=true", route, stream, len(capture.calls), emptyCommentary, phases, partials)
			})
		}
	}
}

type phaseReplayCall struct {
	Request  json.RawMessage `json:"request"`
	Response string          `json:"response"`
	Status   int             `json:"status"`
}
type phaseReplayCapture struct {
	base  http.RoundTripper
	calls []*phaseReplayCall
}
type phaseReplayReadCloser struct {
	io.ReadCloser
	record *phaseReplayCall
}

func (r *phaseReplayReadCloser) Read(p []byte) (int, error) {
	n, e := r.ReadCloser.Read(p)
	r.record.Response += string(p[:n])
	return n, e
}
func (c *phaseReplayCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(raw))
	call := &phaseReplayCall{Request: raw}
	c.calls = append(c.calls, call)
	response, err := c.base.RoundTrip(req)
	if err != nil {
		return response, err
	}
	call.Status = response.StatusCode
	response.Body = &phaseReplayReadCloser{ReadCloser: response.Body, record: call}
	return response, nil
}

func assertPhaseReplayWire(t *testing.T, raw []byte, history []*genai.Content, native bool) {
	t.Helper()
	type message struct{ ID, Phase, Text string }
	var want []message
	for _, content := range history {
		if content.Role != "model" {
			continue
		}
		for _, part := range content.Parts {
			if part == nil || part.Thought {
				continue
			}
			m, ok := converters.MessageMetadataFromPart(part)
			if part.Text == "" && !ok {
				continue
			}
			if m.ID == "" {
				t.Fatalf("cannot prove replay of unlabelled generated text: %#v", part)
			}
			if len(want) > 0 && want[len(want)-1].ID == m.ID && want[len(want)-1].Phase == m.Phase {
				want[len(want)-1].Text += part.Text
			} else {
				want = append(want, message{m.ID, m.Phase, part.Text})
			}
		}
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	var got []message
	if native {
		prompt, _ := body["prompt"].([]any)
		for _, entry := range prompt {
			m := entry.(map[string]any)
			if m["role"] != "assistant" {
				continue
			}
			parts, _ := m["content"].([]any)
			textParts := 0
			for _, entry := range parts {
				p := entry.(map[string]any)
				if p["type"] != "text" {
					continue
				}
				textParts++
				if textParts > 1 {
					t.Fatal("native history merged distinct generated text messages")
				}
				opts, _ := p["providerOptions"].(map[string]any)
				o, _ := opts["openai"].(map[string]any)
				id, _ := o["itemId"].(string)
				phase, _ := o["phase"].(string)
				text, _ := p["text"].(string)
				got = append(got, message{id, phase, text})
			}
		}
	} else {
		input, _ := body["input"].([]any)
		for _, entry := range input {
			m := entry.(map[string]any)
			if m["role"] != "assistant" {
				continue
			}
			id, _ := m["id"].(string)
			phase, _ := m["phase"].(string)
			text := ""
			parts, _ := m["content"].([]any)
			for _, p := range parts {
				v, _ := p.(map[string]any)["text"].(string)
				text += v
			}
			got = append(got, message{id, phase, text})
		}
	}
	if len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("history replay mismatch: got=%+v want=%+v", got, want)
	}
}

// Empty phased messages must survive conversion, JSON persistence and replay,
// even when the extractor rejects the response as empty or ambiguous.
func TestExtractJSONEmptyPhasesRoundTrip(t *testing.T) {
	type msg struct {
		id, phase string
		texts     []string
	}
	for _, tc := range []struct {
		name     string
		messages []msg
		valid    bool
	}{
		{"empty commentary before final", []msg{{"c", "commentary", []string{""}}, {"f", "final_answer", []string{"1"}}}, true},
		{"unphased empty before phased final", []msg{{"other", "", []string{""}}, {"f", "final_answer", []string{"1"}}}, false},
		{"unphased empty after phased final", []msg{{"f", "final_answer", []string{"1"}}, {"other", "", []string{""}}}, false},
		{"same ID empty without phase", []msg{{"f", "final_answer", []string{"1"}}, {"f", "", []string{""}}}, false},
		{"unphased legacy with empty message", []msg{{"m", "", []string{"1", ""}}}, true},
		{"empty distinct first final", []msg{{"f2", "final_answer", []string{""}}, {"f", "final_answer", []string{"1"}}}, false},
		{"two empty distinct finals", []msg{{"f1", "final_answer", []string{""}}, {"f2", "final_answer", []string{""}}}, false},
		{"empty first fragment of same final", []msg{{"f", "final_answer", []string{"", "1"}}}, true},
		{"empty distinct second final", []msg{{"f", "final_answer", []string{"1"}}, {"f2", "final_answer", []string{""}}}, false},
		{"unphased draft then empty final", []msg{{"draft", "", []string{"1"}}, {"f", "final_answer", []string{""}}}, false},
		{"empty final only", []msg{{"f", "final_answer", []string{""}}}, false},
		{"whitespace final", []msg{{"f", "final_answer", []string{" \n"}}}, false},
		{"empty unphased message with ID", []msg{{"m", "", []string{""}}}, false},
		{"empty commentary only", []msg{{"c", "commentary", []string{""}}}, false},
		{"empty fragment of same final", []msg{{"f", "final_answer", []string{"1", ""}}}, true},
	} {
		for _, representation := range []string{"empty-text", "missing-text", "null-text", "empty-content", "null-content", "omitted-content", "empty-refusal", "missing-refusal", "null-refusal", "empty-delta-metadata", "missing-delta-metadata", "null-delta-metadata"} {
			hasEmpty, fragments := false, false
			for _, message := range tc.messages {
				for _, text := range message.texts {
					if text == "" {
						hasEmpty = true
						fragments = fragments || len(message.texts) > 1
					}
				}
			}
			if representation != "empty-text" && !hasEmpty {
				continue
			}
			wholeContent := representation == "empty-content" || representation == "null-content" || representation == "omitted-content"
			if wholeContent && fragments {
				continue
			}
			for _, native := range []bool{false, true} {
				if native && (wholeContent || strings.HasSuffix(representation, "refusal")) {
					continue
				}
				for _, stream := range []bool{false, true} {
					if strings.HasSuffix(representation, "delta-metadata") && (!native || !stream) {
						continue
					}
					t.Run(fmt.Sprintf("%s/%s/native=%t/stream=%t", tc.name, representation, native, stream), func(t *testing.T) {
						var output, blocks []any
						var events []string
						for i, m := range tc.messages {
							var content []any
							for j, text := range m.texts {
								block := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
								if text == "" {
									switch representation {
									case "missing-text":
										delete(block, "text")
									case "null-text":
										block["text"] = nil
									case "empty-refusal", "missing-refusal", "null-refusal":
										block = map[string]any{"type": "refusal"}
										switch representation {
										case "empty-refusal":
											block["refusal"] = ""
										case "null-refusal":
											block["refusal"] = nil
										}
									}
								}
								content = append(content, block)
								metadata := map[string]any{"openai": map[string]any{"itemId": m.id, "phase": m.phase}}
								nativeBlock := map[string]any{"type": "text", "text": text, "providerMetadata": metadata}
								if text == "" && representation == "missing-text" {
									delete(nativeBlock, "text")
								}
								if text == "" && representation == "null-text" {
									nativeBlock["text"] = nil
								}
								blocks = append(blocks, nativeBlock)
								textID := fmt.Sprintf("text_%d_%d", i, j)
								startPart := map[string]any{"type": "text-start", "id": textID, "providerMetadata": metadata}
								if text == "" && strings.HasSuffix(representation, "delta-metadata") {
									delete(startPart, "providerMetadata")
								}
								start, _ := json.Marshal(startPart)
								events = append(events, string(start))
								deltaPart := map[string]any{"type": "text-delta", "id": textID, "delta": text}
								if text == "" && strings.HasSuffix(representation, "delta-metadata") {
									deltaPart["providerMetadata"] = metadata
								}
								if text == "" && (representation == "missing-text" || representation == "missing-delta-metadata") {
									delete(deltaPart, "delta")
								}
								if text == "" && (representation == "null-text" || representation == "null-delta-metadata") {
									deltaPart["delta"] = nil
								}
								delta, _ := json.Marshal(deltaPart)
								events = append(events, string(delta))
								end, _ := json.Marshal(map[string]any{"type": "text-end", "id": textID})
								events = append(events, string(end))
							}
							wireMessage := map[string]any{"type": "message", "id": m.id, "phase": m.phase, "role": "assistant", "status": "completed", "content": content}
							if len(m.texts) == 1 && m.texts[0] == "" {
								switch representation {
								case "empty-content":
									wireMessage["content"] = []any{}
								case "null-content":
									wireMessage["content"] = nil
								case "omitted-content":
									delete(wireMessage, "content")
								}
							}
							output = append(output, wireMessage)
						}
						response := map[string]any{"id": "resp_empty", "status": "completed", "output": output}
						raw, _ := json.Marshal(response)
						if native {
							raw, _ = json.Marshal(map[string]any{"content": blocks, "finishReason": map[string]any{"unified": "stop"}})
						}
						capture := &phaseReplayCapture{base: phaseFixtureTransport(func(r *http.Request) (*http.Response, error) {
							body, contentType := string(raw), "application/json"
							if stream {
								contentType = "text/event-stream"
								if native {
									body = ""
									for _, event := range events {
										body += "data: " + event + "\n\n"
									}
									body += "data: {\"type\":\"finish\",\"finishReason\":{\"unified\":\"stop\"}}\n\n"
								} else {
									event, _ := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": 1, "response": response})
									body = "event: response.completed\ndata: " + string(event) + "\n\n"
								}
							}
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
						})}
						client := &http.Client{Transport: capture}
						mc := adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "gpt-5.6-luna"}
						var llm model.LLM
						var err error
						if native {
							llm, err = adkvercel.NewModel(adkvercel.Config{APIKey: "synthetic", HTTPClient: client, Model: mc})
						} else {
							sdk := openai.NewClient(option.WithAPIKey("synthetic"), option.WithHTTPClient(client), option.WithMaxRetries(0))
							llm, err = adkopenai.NewModel(adkopenai.Config{Client: sdk, Model: mc})
						}
						if err != nil {
							t.Fatal(err)
						}
						var final *genai.Content
						for r, e := range llm.GenerateContent(t.Context(), &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("fixture", genai.RoleUser)}}, stream) {
							if e != nil {
								t.Fatal(e)
							}
							if r != nil && !r.Partial {
								final = r.Content
							}
						}
						if final == nil {
							t.Fatal("no terminal content")
						}
						fragments := 0
						for _, m := range tc.messages {
							fragments += len(m.texts)
						}
						if len(final.Parts) != fragments {
							t.Fatalf("lost message parts: got%d want%d", len(final.Parts), fragments)
						}
						index := 0
						for _, original := range tc.messages {
							for _, text := range original.texts {
								part := final.Parts[index]
								metadata, ok := converters.MessageMetadataFromPart(part)
								if !ok || part.Text != text || metadata.ID != original.id || metadata.Phase != original.phase {
									t.Fatalf("fragment %d changed: text=%q metadata=%+v; want text=%q id=%s phase=%s", index, part.Text, metadata, text, original.id, original.phase)
								}
								index++
							}
						}
						data, err := json.Marshal(final)
						if err != nil {
							t.Fatal(err)
						}
						var saved genai.Content
						if err = json.Unmarshal(data, &saved); err != nil {
							t.Fatal(err)
						}
						value, err := ExtractJSON[int](&saved)
						if tc.valid {
							if err != nil || value == nil || *value != 1 {
								t.Fatalf("value=%v error=%v", value, err)
							}
						} else if err == nil {
							t.Fatalf("accepted incomplete/ambiguous output: %s", data)
						}
						history := []*genai.Content{&saved, genai.NewContentFromText("continue", genai.RoleUser)}
						for _, e := range llm.GenerateContent(t.Context(), &model.LLMRequest{Contents: history}, stream) {
							if e != nil {
								t.Fatal(e)
							}
						}
						if len(capture.calls) != 2 {
							t.Fatalf("replay made%d requests", len(capture.calls))
						}
						assertPhaseReplayWire(t, capture.calls[1].Request, history, native)
						after, _ := json.Marshal(&saved)
						if string(after) != string(data) {
							t.Fatal("extraction/replay changed saved history")
						}
					})
				}
			}
		}
	}
}

type phaseFixtureTransport func(*http.Request) (*http.Response, error)

func (f phaseFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// No message facts and malformed blocks must not become invented empty finals.
func TestExtractJSONEmptyAndMalformedEnvelopes(t *testing.T) {
	for _, kind := range []string{"empty-array", "null", "omitted", "null-block", "empty-object-block"} {
		for _, native := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/native=%t/stream=%t", kind, native, stream), func(t *testing.T) {
					response := map[string]any{"status": "completed"}
					field := "output"
					if native {
						field = "content"
						response = map[string]any{"finishReason": map[string]any{"unified": "stop"}}
					}
					switch kind {
					case "empty-array":
						response[field] = []any{}
					case "null":
						response[field] = nil
					case "null-block", "empty-object-block":
						var block any
						if kind == "empty-object-block" {
							block = map[string]any{}
						}
						if native {
							response[field] = []any{block}
						} else {
							response[field] = []any{map[string]any{"type": "message", "id": "f", "phase": "final_answer", "content": []any{block}}}
						}
					}
					raw, _ := json.Marshal(response)
					client := &http.Client{Transport: phaseFixtureTransport(func(r *http.Request) (*http.Response, error) {
						body, contentType := string(raw), "application/json"
						if stream {
							contentType = "text/event-stream"
							if native {
								body = "data: {\"type\":\"finish\",\"finishReason\":{\"unified\":\"stop\"}}\n\n"
								if kind == "null-block" {
									body = "data: null\n\n" + body
								}
								if kind == "empty-object-block" {
									body = "data: {}\n\n" + body
								}
							} else {
								event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": response})
								body = "event: response.completed\ndata: " + string(event) + "\n\n"
							}
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})}
					mc := adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", RequestModel: "gpt-5.6-luna"}
					var llm model.LLM
					var err error
					if native {
						llm, err = adkvercel.NewModel(adkvercel.Config{APIKey: "synthetic", HTTPClient: client, Model: mc})
					} else {
						sdk := openai.NewClient(option.WithAPIKey("synthetic"), option.WithHTTPClient(client), option.WithMaxRetries(0))
						llm, err = adkopenai.NewModel(adkopenai.Config{Client: sdk, Model: mc})
					}
					if err != nil {
						t.Fatal(err)
					}
					observed := false
					for r, e := range llm.GenerateContent(t.Context(), &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("fixture", genai.RoleUser)}}, stream) {
						if e != nil {
							observed = true
							continue
						}
						if r == nil || r.Partial {
							continue
						}
						observed = true
						if _, err := ExtractJSON[int](r.Content); err == nil {
							t.Fatal("accepted an empty or malformed envelope")
						}
						if r.Content != nil && len(r.Content.Parts) != 0 {
							t.Fatalf("invented message facts: %#v", r.Content)
						}
					}
					if !observed {
						t.Fatal("no terminal outcome")
					}
				})
			}
		}
	}
}
