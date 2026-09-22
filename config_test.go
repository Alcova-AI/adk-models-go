// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels_test

import (
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"google.golang.org/genai"
)

func TestRawOptionsCannotOverrideSharedControls(t *testing.T) {
	for _, key := range []string{"store", "reasoningEffort", "reasoning_effort", "thinking", "thinkingConfig", "effort", "output_config", "promptCacheKey", "zero-data-retention"} {
		cfg := adkmodels.VercelConfig{ProviderOptions: map[string]map[string]any{"openai": {key: "override"}}}
		if err := cfg.Validate(); err == nil {
			t.Errorf("reserved provider key %s accepted", key)
		}
	}
	for _, key := range []string{"zeroDataRetention", "zero_data_retention", "only", "caching", "providerTimeouts", "user"} {
		cfg := adkmodels.VercelConfig{GatewayOptions: map[string]any{key: "override"}}
		if err := cfg.Validate(); err == nil {
			t.Errorf("reserved gateway key %s accepted", key)
		}
	}
	if err := (adkmodels.VercelConfig{ProviderOptions: map[string]map[string]any{"custom": {"futureOption": true}}, GatewayOptions: map[string]any{"futureRouting": true}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayValidationIsRouteSpecific(t *testing.T) {
	cfg := adkmodels.ModelConfig{CanonicalModel: "mimo-v2.6-pro", RequestModel: "xiaomi/mimo-v2.6-pro"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("direct adapter validation accepted an unknown family")
	}
	if err := cfg.ValidateGateway(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*adkmodels.ModelConfig){
		func(c *adkmodels.ModelConfig) { c.DefaultMaxOutputTokens = -1 },
		func(c *adkmodels.ModelConfig) { c.Reasoning.DefaultLevel = genai.ThinkingLevelHigh },
		func(c *adkmodels.ModelConfig) {
			c.Vercel = &adkmodels.VercelConfig{GatewayOptions: map[string]any{"zeroDataRetention": false}}
		},
	} {
		invalid := cfg
		change(&invalid)
		if err := invalid.ValidateGateway(); err == nil {
			t.Fatalf("accepted invalid Gateway configuration: %+v", invalid)
		}
	}
}
