// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package family

import (
	"testing"

	"google.golang.org/genai"
)

func TestApprovedMappings(t *testing.T) {
	levels := []genai.ThinkingLevel{genai.ThinkingLevelMinimal, genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh, XHigh, Max}
	cases := []struct {
		family   Family
		efforts  []string
		thinking []string
	}{
		{OpenAI, []string{"none", "low", "medium", "high", "xhigh", "xhigh"}, []string{"", "", "", "", "", ""}},
		{Anthropic, []string{"low", "low", "medium", "high", "xhigh", "max"}, []string{"disabled", "adaptive", "adaptive", "adaptive", "adaptive", "adaptive"}},
		{Gemini, []string{"MINIMAL", "LOW", "MEDIUM", "HIGH", "HIGH", "HIGH"}, []string{"", "", "", "", "", ""}},
		{ZAI, []string{"low", "low", "high", "max", "max", "max"}, []string{"enabled", "enabled", "enabled", "enabled", "enabled", "enabled"}},
	}
	for _, tt := range cases {
		for i, level := range levels {
			t.Run(string(tt.family)+"/"+string(level), func(t *testing.T) {
				got, err := Map(tt.family, level)
				if err != nil || got.Effort != tt.efforts[i] || got.Thinking != tt.thinking[i] {
					t.Fatalf("got %+v, %v", got, err)
				}
			})
		}
		for _, level := range []genai.ThinkingLevel{"", genai.ThinkingLevelUnspecified} {
			got, err := Map(tt.family, level)
			if err != nil || got != (Reasoning{}) {
				t.Fatalf("unset %s: %+v %v", tt.family, got, err)
			}
		}
	}
	if _, err := Map(OpenAI, "ULTRA"); err == nil {
		t.Fatal("unknown level accepted")
	}
}

func TestNames(t *testing.T) {
	for _, tt := range []struct {
		name string
		want Family
	}{
		{"  GPT-5.6-luna  ", OpenAI}, {"o1", OpenAI}, {"o1-mini", OpenAI}, {"o3", OpenAI}, {"o3-pro", OpenAI}, {"o4", OpenAI}, {"o4-mini", OpenAI}, {"claude-opus-4-7", Anthropic}, {"Gemini-3-pro", Gemini}, {"GLM-5.3", ZAI},
	} {
		got, err := Detect(tt.name)
		if err != nil || got != tt.want {
			t.Errorf("%q: %q %v", tt.name, got, err)
		}
	}
	for _, name := range []string{"", "custom", "openai/gpt-5.6-luna", "o5", "o11"} {
		if _, err := Detect(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, tt := range []struct {
		name string
		bad  bool
	}{
		{"openai/gpt-5.6-luna", false}, {" OPENAI/GPT-5.6-LUNA ", false}, {"anthropic/claude-opus-4-7", true}, {"openai/claude-opus-4-7", true}, {"my-deployment", false}, {"claude-opus-4-7", true}, {"openai/my-deployment", false},
	} {
		if err := ValidateRequest(OpenAI, tt.name); (err != nil) != tt.bad {
			t.Errorf("%q: %v", tt.name, err)
		}
	}
}
