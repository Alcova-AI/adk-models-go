// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package gateway

import (
	"encoding/json"
	"testing"

	"github.com/Alcova-AI/adk-models-go/internal/family"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/genai"

	adkmodels "github.com/Alcova-AI/adk-models-go"
)

func TestGatewayOptionsApplyAllNativeFields(t *testing.T) {

	options := adkmodels.VercelConfig{
		BYOK: map[string][]map[string]any{
			"azure": {{"apiKey": "secret"}},
		},
		Caching:                adkmodels.GatewayCachingAuto,
		DisallowPromptTraining: true,
		Has:                    []adkmodels.GatewayCapability{adkmodels.GatewayCapabilityImplicitCaching, adkmodels.GatewayCapabilityVision},
		IdempotencyKey:         "batch-1",
		Models:                 []string{"openai/gpt-5.6-sol"},
		Only:                   []string{"azure"},
		Order:                  []string{"azure", "openai"},
		ProviderTimeouts:       &adkmodels.GatewayProviderTimeouts{BYOK: map[string]int{"azure": 2_000}},
		QuotaEntityID:          "org-1",
		ServiceTier:            adkmodels.GatewayServiceTierPriority,
		Sort:                   adkmodels.GatewaySortTTFT,
		Tags:                   []string{"operator", "local"},
		User:                   "user-1",
		ZeroDataRetention:      true,
		GatewayOptions:         map[string]any{"futureOption": "preserved"},
	}
	encoded, err := gatewayOptions(options)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	raw, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("marshal gateway options: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal gateway options: %v", err)
	}
	for _, key := range []string{
		"byok", "caching", "disallowPromptTraining", "has", "idempotencyKey",
		"models", "only", "order", "providerTimeouts", "quotaEntityId",
		"serviceTier", "sort", "tags", "user", "zeroDataRetention", "futureOption",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("gateway option %q missing from %#v", key, got)
		}
	}
}

func TestGatewayOptionsRejectInvalidEnums(t *testing.T) {
	tests := []adkmodels.VercelConfig{
		{Caching: "manual"},
		{Has: []adkmodels.GatewayCapability{"audio"}},
		{ServiceTier: "standard"},
		{Sort: "latency"},
	}
	for _, options := range tests {
		if err := options.Validate(); err == nil {
			t.Errorf("Apply(%#v) succeeded, want error", options)
		}
	}
}

func TestGatewayOptionsExplicitlyAllowsRetentionByDefault(t *testing.T) {
	encoded, err := gatewayOptions(adkmodels.VercelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if encoded["zeroDataRetention"] != false {
		t.Fatalf("default privacy option: %#v", encoded)
	}
}

func TestTypedOpenAIControlsReachGateway(t *testing.T) {
	options, err := Options(adkmodels.VercelConfig{}, family.OpenAI, genai.ThinkingLevelMinimal, true, adkmodels.OpenAIReasoningConfig{Context: shared.ReasoningContextAllTurns, Mode: shared.ReasoningModePro, Summary: shared.ReasoningSummaryDetailed})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"store": false, "reasoningEffort": "none", "reasoningContext": "all_turns", "reasoningMode": "pro", "reasoningSummary": "detailed"} {
		if options["openai"][key] != want {
			t.Errorf("%s: %v", key, options["openai"][key])
		}
	}
}
