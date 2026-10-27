package converters

import (
	"encoding/json"
	"google.golang.org/genai"
	"testing"
)

func TestMessageMetadataSurvivesJSON(t *testing.T) {
	part := &genai.Part{Text: "unchanged", PartMetadata: map[string]any{"existing": "keep"}}
	want := MessageMetadata{ID: "msg_final", Phase: MessagePhaseFinalAnswer}
	SetMessageMetadata(part, want)
	raw, err := json.Marshal(part)
	if err != nil {
		t.Fatal(err)
	}
	var restored genai.Part
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	got, ok := MessageMetadataFromPart(&restored)
	if !ok || got != want || restored.Text != "unchanged" || restored.PartMetadata["existing"] != "keep" {
		t.Fatalf("round-trip = %#v, metadata %#v", restored, got)
	}
}

func TestEmptyMessageMetadataLeavesPartUnchanged(t *testing.T) {
	p := &genai.Part{Text: "hello"}
	SetMessageMetadata(p, MessageMetadata{})
	if p.PartMetadata != nil {
		t.Fatalf("unexpected metadata: %#v", p.PartMetadata)
	}
	if _, ok := MessageMetadataFromPart(nil); ok {
		t.Fatal("nil part has metadata")
	}
}
