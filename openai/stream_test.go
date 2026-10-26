// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkopenai

import (
	"encoding/json"
	"testing"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
	"github.com/openai/openai-go/v3/responses"
)

func TestStreamingTextDeltaPreservesMessagePhase(t *testing.T) {
	translator := newStreamTranslator()
	emitted := 0
	for _, raw := range []string{
		`{"type":"response.output_item.added","item":{"type":"message","id":"msg_c","phase":"commentary"}}`,
		`{"type":"response.output_text.delta","item_id":"msg_c","delta":"{\"section\":\"\"}"}`,
	} {
		var event responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		got, err := translator.process(event)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			emitted++
			p := got.Candidates[0].Content.Parts[0]
			m, ok := converters.MessageMetadataFromPart(p)
			if !ok || m.ID != "msg_c" || m.Phase != "commentary" || p.Text != `{"section":""}` {
				t.Fatalf("part=%#v metadata=%#v", p, m)
			}
		}
	}
	if emitted != 1 {
		t.Fatalf("emitted %d text responses, want 1", emitted)
	}
}
