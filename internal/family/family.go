// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

// Package family owns model identity and the approved reasoning mappings.
package family

import (
	"fmt"
	"strings"

	"google.golang.org/genai"
)

type Family string

const (
	OpenAI    Family              = "openai"
	Anthropic Family              = "anthropic"
	Gemini    Family              = "google"
	ZAI       Family              = "zai"
	XHigh     genai.ThinkingLevel = "XHIGH"
	Max       genai.ThinkingLevel = "MAX"
)

func Detect(name string) (Family, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(name, "gpt-"), name == "o1", strings.HasPrefix(name, "o1-"), name == "o3", strings.HasPrefix(name, "o3-"), name == "o4", strings.HasPrefix(name, "o4-"):
		return OpenAI, nil
	case strings.HasPrefix(name, "claude-"):
		return Anthropic, nil
	case strings.HasPrefix(name, "gemini-"):
		return Gemini, nil
	case strings.HasPrefix(name, "glm-"):
		return ZAI, nil
	default:
		return "", fmt.Errorf("unrecognised canonical model %q", name)
	}
}

// ValidateRequest checks recognised identities without rewriting endpoint aliases.
func ValidateRequest(canonical Family, request string) error {
	name := strings.ToLower(strings.TrimSpace(request))
	prefix, suffix, qualified := strings.Cut(name, "/")
	if !qualified {
		return compareKnown(canonical, name)
	}
	namespace := Family(prefix)
	switch namespace {
	case OpenAI, Anthropic, Gemini, ZAI:
		if err := compareKnown(namespace, suffix); err != nil {
			return err
		}
		if namespace != canonical {
			return fmt.Errorf("request model family %q differs from canonical family %q", namespace, canonical)
		}
	}
	return nil
}

func compareKnown(expected Family, name string) error {
	actual, err := Detect(name)
	if err == nil && actual != expected {
		return fmt.Errorf("request model family %q differs from expected family %q", actual, expected)
	}
	return nil
}

func ValidLevel(level genai.ThinkingLevel) bool {
	switch level {
	case "", genai.ThinkingLevelUnspecified, genai.ThinkingLevelMinimal, genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh, XHigh, Max:
		return true
	default:
		return false
	}
}

type Reasoning struct {
	Effort   string
	Thinking string
}

func Map(f Family, level genai.ThinkingLevel) (Reasoning, error) {
	if !ValidLevel(level) {
		return Reasoning{}, fmt.Errorf("unsupported thinking level %q", level)
	}
	if level == "" || level == genai.ThinkingLevelUnspecified {
		return Reasoning{}, nil
	}
	switch f {
	case OpenAI:
		return Reasoning{Effort: openAI(level)}, nil
	case Anthropic:
		if level == genai.ThinkingLevelMinimal {
			return Reasoning{Effort: "low", Thinking: "disabled"}, nil
		}
		return Reasoning{Effort: strings.ToLower(string(level)), Thinking: "adaptive"}, nil
	case Gemini:
		if level == XHigh || level == Max {
			level = genai.ThinkingLevelHigh
		}
		return Reasoning{Effort: string(level)}, nil
	case ZAI:
		return Reasoning{Effort: zai(level), Thinking: "enabled"}, nil
	default:
		return Reasoning{}, fmt.Errorf("unsupported model family %q", f)
	}
}

func openAI(level genai.ThinkingLevel) string {
	switch level {
	case genai.ThinkingLevelMinimal:
		return "none"
	case Max:
		return "xhigh"
	default:
		return strings.ToLower(string(level))
	}
}

func zai(level genai.ThinkingLevel) string {
	switch level {
	case genai.ThinkingLevelMinimal, genai.ThinkingLevelLow:
		return "low"
	case genai.ThinkingLevelMedium:
		return "high"
	default:
		return "max"
	}
}
