// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package converters

import "google.golang.org/genai"

// MessageMetadataKey scopes these message facts to OpenAI Responses, including
// OpenAI messages carried by the native Vercel transport.
const MessageMetadataKey = "adkmodels.openai.message"

const (
	MessagePhaseCommentary  = "commentary"
	MessagePhaseFinalAnswer = "final_answer"
)

// MessageMetadata identifies the OpenAI message that produced a text part.
// Adapters preserve these facts without selecting or removing output.
type MessageMetadata struct {
	ID    string `json:"id,omitempty"`
	Phase string `json:"phase,omitempty"`
}

// SetMessageMetadata attaches optional message facts without replacing other metadata.
// Map values survive JSON serialisation of persisted genai.Parts.
func SetMessageMetadata(part *genai.Part, value MessageMetadata) {
	if part == nil || (value.ID == "" && value.Phase == "") {
		return
	}
	if part.PartMetadata == nil {
		part.PartMetadata = make(map[string]any)
	}
	fields := make(map[string]any)
	if value.ID != "" {
		fields["id"] = value.ID
	}
	if value.Phase != "" {
		fields["phase"] = value.Phase
	}
	part.PartMetadata[MessageMetadataKey] = fields
}

// MessageMetadataFromPart reads live or JSON-round-tripped message facts.
func MessageMetadataFromPart(part *genai.Part) (MessageMetadata, bool) {
	if part == nil {
		return MessageMetadata{}, false
	}
	fields, ok := part.PartMetadata[MessageMetadataKey].(map[string]any)
	if !ok {
		return MessageMetadata{}, false
	}
	id, _ := fields["id"].(string)
	phase, _ := fields["phase"].(string)
	return MessageMetadata{ID: id, Phase: phase}, true
}
