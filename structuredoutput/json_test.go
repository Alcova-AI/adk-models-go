package structuredoutput_test

import (
	"encoding/json"
	"reflect"
	"testing"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/Alcova-AI/adk-models-go/structuredoutput"
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
			got, err := structuredoutput.ExtractJSON[answer](tc.content)
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
