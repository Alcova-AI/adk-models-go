// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package converters

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"google.golang.org/genai"
)

func TestContentsToResponseInputMessageRoundTrip(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		name := "memory"
		if persisted {
			name = "persisted"
		}
		t.Run(name, func(t *testing.T) {
			var response responses.Response
			if err := json.Unmarshal([]byte(`{"id":"resp_phases","status":"completed","output":[{"type":"message","id":"msg_c","phase":"commentary","content":[{"type":"output_text","text":"checking"}]},{"type":"message","id":"msg_f","phase":"final_answer","content":[{"type":"output_text","text":"answer "},{"type":"output_text","text":"complete"}]},{"type":"message","id":"msg_f2","phase":"final_answer","content":[{"type":"output_text","text":"another message"}]}]}`), &response); err != nil {
				t.Fatal(err)
			}
			converted, err := ResponseToLLMResponse(&response, false)
			if err != nil {
				t.Fatal(err)
			}
			content := converted.Content
			if persisted {
				data, err := json.Marshal(content)
				if err != nil {
					t.Fatal(err)
				}
				content = new(genai.Content)
				if err := json.Unmarshal(data, content); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := json.Marshal(content)
			input, err := ContentsToResponseInput([]*genai.Content{content})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var wire []struct {
				ID      string `json:"id"`
				Phase   string `json:"phase"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire) != 3 {
				t.Fatalf("want 3 distinct messages, got %s", data)
			}
			ids := []string{"msg_c", "msg_f", "msg_f2"}
			phases := []string{"commentary", "final_answer", "final_answer"}
			texts := [][]string{{"checking"}, {"answer ", "complete"}, {"another message"}}
			for i, message := range wire {
				var got []string
				for _, part := range message.Content {
					got = append(got, part.Text)
				}
				if message.ID != ids[i] || message.Phase != phases[i] || !reflect.DeepEqual(got, texts[i]) {
					t.Fatalf("message %d: %s", i, data)
				}
			}
			after, _ := json.Marshal(content)
			if string(before) != string(after) {
				t.Fatal("history mutated")
			}
		})
	}
}

func TestContentsToResponseInputLegacyAndPhaseBoundaries(t *testing.T) {
	parts := []*genai.Part{{Text: "legacy one"}, {Text: "legacy two"}, {Text: "progress"}, {Text: "final"}, {Text: "legacy after"}}
	SetMessageMetadata(parts[2], MessageMetadata{Phase: "commentary"})
	SetMessageMetadata(parts[3], MessageMetadata{Phase: "final_answer"})
	input, err := ContentsToResponseInput([]*genai.Content{{Role: "model", Parts: parts}})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 4 {
		t.Fatalf("want 4 messages, got %#v", input)
	}
	if len(input[0].OfOutputMessage.Content) != 2 || input[0].OfOutputMessage.Phase != "" || input[0].OfOutputMessage.ID != "" {
		t.Fatal("legacy grouping changed")
	}
	if input[1].OfOutputMessage.Phase != "commentary" || input[2].OfOutputMessage.Phase != "final_answer" || input[3].OfOutputMessage.Phase != "" {
		t.Fatal("phase boundaries lost")
	}
	// User text must never acquire assistant-only fields.
	user := &genai.Part{Text: "question"}
	SetMessageMetadata(user, MessageMetadata{ID: "untrusted", Phase: "commentary"})
	input, err = ContentsToResponseInput([]*genai.Content{{Role: "user", Parts: []*genai.Part{user}}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(input)
	var wire []map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire[0]["id"]; ok {
		t.Fatalf("user id leaked: %s", data)
	}
	if _, ok := wire[0]["phase"]; ok {
		t.Fatalf("user phase leaked: %s", data)
	}
}

func TestContentsToResponseInputPhasesKeepReasoningAndToolOrder(t *testing.T) {
	var response responses.Response
	if err := json.Unmarshal([]byte(`{"id":"resp_tools","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"opaque","summary":[]},{"type":"message","id":"msg_c","phase":"commentary","content":[{"type":"output_text","text":"checking"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}]}`), &response); err != nil {
		t.Fatal(err)
	}
	converted, err := ResponseToLLMResponse(&response, false)
	if err != nil {
		t.Fatal(err)
	}
	input, err := ContentsToResponseInput([]*genai.Content{converted.Content, {Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "call_1", Name: "lookup", Response: map[string]any{"result": "found"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 4 || input[0].OfReasoning == nil || input[1].OfOutputMessage == nil || input[2].OfFunctionCall == nil || input[3].OfFunctionCallOutput == nil {
		t.Fatalf("tool/reasoning order changed: %#v", input)
	}
	if input[1].OfOutputMessage.ID != "msg_c" || input[1].OfOutputMessage.Phase != "commentary" {
		t.Fatal("commentary metadata lost before tool call")
	}
	wire, err := json.Marshal(input[0])
	if err != nil {
		t.Fatal(err)
	}
	var reasoning map[string]any
	if err := json.Unmarshal(wire, &reasoning); err != nil {
		t.Fatal(err)
	}
	if reasoning["encrypted_content"] != "opaque" || reasoning["id"] != "rs_1" {
		t.Fatalf("reasoning changed: %s", wire)
	}
	if input[2].OfFunctionCall.CallID != "call_1" || input[3].OfFunctionCallOutput.CallID.Value != "call_1" {
		t.Fatal("tool linkage changed")
	}
}

func TestContentsToResponseInputEmptyMessageMetadata(t *testing.T) {
	for _, metadata := range []MessageMetadata{{Phase: "commentary"}, {ID: "msg_empty"}} {
		part := &genai.Part{}
		SetMessageMetadata(part, metadata)
		input, err := ContentsToResponseInput([]*genai.Content{{Role: "model", Parts: []*genai.Part{part}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(input) != 1 || input[0].OfOutputMessage == nil {
			t.Fatalf("missing empty message: %#v", input)
		}
		m := input[0].OfOutputMessage
		if m.ID != metadata.ID || string(m.Phase) != metadata.Phase || len(m.Content) != 1 || m.Content[0].OfOutputText.Text != "" {
			t.Fatalf("empty message changed: %#v", m)
		}
	}
	// Unrelated metadata must not turn an unsupported empty part into a message.
	_, err := ContentsToResponseInput([]*genai.Content{{Role: "model", Parts: []*genai.Part{{PartMetadata: map[string]any{"unrelated": true}}}}})
	if err == nil {
		t.Fatal("accepted an unsupported empty part")
	}
}
