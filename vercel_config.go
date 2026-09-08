// Copyright 2025 Alcova AI
// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkmodels

import (
	"fmt"
	"strings"
)

type GatewayCaching string

const GatewayCachingAuto GatewayCaching = "auto"

type GatewayCapability string

const (
	GatewayCapabilityImplicitCaching GatewayCapability = "implicit-caching"
	GatewayCapabilityVision          GatewayCapability = "vision"
)

type GatewayServiceTier string

const (
	GatewayServiceTierFlex     GatewayServiceTier = "flex"
	GatewayServiceTierPriority GatewayServiceTier = "priority"
)

type GatewaySort string

const (
	GatewaySortCost GatewaySort = "cost"
	GatewaySortTPS  GatewaySort = "tps"
	GatewaySortTTFT GatewaySort = "ttft"
)

type GatewayProviderTimeouts struct {
	BYOK map[string]int `json:"byok,omitempty"`
}

// VercelConfig owns all gateway behaviour. ZeroDataRetention is opt-in. Raw
// options may extend the wire contract but cannot override typed controls.
type VercelConfig struct {
	BYOK                   map[string][]map[string]any
	Caching                GatewayCaching
	DisallowPromptTraining bool
	Has                    []GatewayCapability
	IdempotencyKey         string
	Models                 []string
	Only                   []string
	Order                  []string
	ProviderTimeouts       *GatewayProviderTimeouts
	QuotaEntityID          string
	ServiceTier            GatewayServiceTier
	Sort                   GatewaySort
	Tags                   []string
	User                   string
	ZeroDataRetention      bool
	// ProviderOptions contains provider-specific namespaces, excluding gateway.
	ProviderOptions map[string]map[string]any
	// GatewayOptions holds additional gateway fields without overriding typed ones.
	GatewayOptions map[string]any
}

func (c VercelConfig) Validate() error {
	if err := c.validateEnums(); err != nil {
		return err
	}
	if err := validateProviders("only", c.Only); err != nil {
		return err
	}
	if err := validateProviders("order", c.Order); err != nil {
		return err
	}
	for namespace, values := range c.ProviderOptions {
		if err := validateName("provider option namespace", namespace); err != nil {
			return err
		}
		if strings.EqualFold(namespace, "gateway") {
			return fmt.Errorf("vercel provider option namespace %q is reserved", namespace)
		}
		for key := range values {
			if err := validateName("provider option key", key); err != nil {
				return err
			}
			if reservedProviderKey(key) {
				return fmt.Errorf("vercel provider option %s.%s conflicts with adapter-owned reasoning, caching, or data policy", namespace, key)
			}
		}
	}
	for key := range c.GatewayOptions {
		if err := validateName("gateway option key", key); err != nil {
			return err
		}
		if reservedGatewayKey(key) {
			return fmt.Errorf("vercel gateway option %s conflicts with a typed setting", key)
		}
	}
	return nil
}

func (c VercelConfig) validateEnums() error {
	if c.Caching != "" && c.Caching != GatewayCachingAuto {
		return fmt.Errorf("unsupported gateway caching mode %q", c.Caching)
	}
	for _, capability := range c.Has {
		switch capability {
		case GatewayCapabilityImplicitCaching, GatewayCapabilityVision:
		default:
			return fmt.Errorf("unsupported gateway capability %q", capability)
		}
	}
	switch c.ServiceTier {
	case "", GatewayServiceTierFlex, GatewayServiceTierPriority:
	default:
		return fmt.Errorf("unsupported gateway service tier %q", c.ServiceTier)
	}
	switch c.Sort {
	case "", GatewaySortCost, GatewaySortTPS, GatewaySortTTFT:
	default:
		return fmt.Errorf("unsupported gateway routing sort %q", c.Sort)
	}
	return nil
}

func validateName(kind, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("vercel %s must not be empty", kind)
	}
	if trimmed != value {
		return fmt.Errorf("vercel %s %q must not have leading or trailing whitespace", kind, value)
	}
	return nil
}

func validateProviders(field string, providers []string) error {
	seen := make(map[string]bool, len(providers))
	for _, provider := range providers {
		if err := validateName("routing "+field+" provider", provider); err != nil {
			return err
		}
		if seen[provider] {
			return fmt.Errorf("vercel routing %s contains duplicate provider %q", field, provider)
		}
		seen[provider] = true
	}
	return nil
}

func normalisedOptionKey(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
}
func reservedProviderKey(key string) bool {
	switch normalisedOptionKey(key) {
	case "gateway", "thinking", "thinkingconfig", "reasoning", "reasoningeffort", "effort", "reasoningsummary", "reasoningcontext", "reasoningmode", "outputconfig", "cachecontrol", "zerodataretention", "store", "promptcachekey", "promptcacheoptions", "promptcachebreakpoint":
		return true
	default:
		return false
	}
}
func reservedGatewayKey(key string) bool {
	switch normalisedOptionKey(key) {
	case "byok", "caching", "disallowprompttraining", "has", "idempotencykey", "models", "only", "order", "providertimeouts", "quotaentityid", "servicetier", "sort", "tags", "user", "zerodataretention":
		return true
	default:
		return false
	}
}
