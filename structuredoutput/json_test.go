package structuredoutput

import (
	"encoding/json"
	"reflect"
	"testing"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
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
