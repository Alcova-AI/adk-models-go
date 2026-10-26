package structuredoutput_test

import (
	"encoding/json"
	"reflect"
	"testing"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/Alcova-AI/adk-models-go/structuredoutput"
	"google.golang.org/genai"
)

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
			got, err := structuredoutput.TextCandidates(&restored)
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
	if _, err := structuredoutput.TextCandidates(nil); err == nil {
		t.Fatal("nil content accepted")
	}
}
