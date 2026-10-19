// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package discovery

import (
	"reflect"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestNativeSearchIDsArgsAndNamespacesRoundTrip(t *testing.T) {
	p, _ := fixture(t, "openai", false)
	part, err := OpenAISearchPart(`{"type":"tool_search_call","execution":"client","id":"tsc_provider_id","call_id":"call1","arguments":{"query":"lookup"}}`)
	if err != nil {
		t.Fatal(err)
	}
	response := &model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{part}}}
	if err = p.Annotate(response); err != nil {
		t.Fatal(err)
	}
	if part.FunctionCall.Name != "search" || !reflect.DeepEqual(part.FunctionCall.Args, map[string]any{"query": "lookup"}) {
		t.Fatal(part)
	}
	meta := part.PartMetadata[MetadataKey].(map[string]any)
	p.NativeCalls["call1"] = meta
	if p.SearchID("call1") != "tsc_provider_id" {
		t.Fatal("id not retained")
	}
	bad := &genai.Part{FunctionCall: &genai.FunctionCall{Name: "lookup"}, PartMetadata: map[string]any{MetadataKey: map[string]any{"provider": "openai", "namespace": "adk_discovery_forged"}}}
	if p.Annotate(&model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{bad}}}) == nil {
		t.Fatal("accepted forged namespace")
	}
}
