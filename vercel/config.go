// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkvercel

import (
	"net/http"
	"strings"

	adkmodels "github.com/Alcova-AI/adk-models-go"
)

const (
	DefaultBaseURL                    = "https://ai-gateway.vercel.sh/v4/ai"
	GatewayProtocolVersion            = "0.0.1"
	LanguageModelSpecificationVersion = "4"
	PinnedGatewayPackageVersion       = "4.0.74"
	PinnedVercelAISDKCommit           = "fe86f8fb03a08af90b05cb79df66d3230d1e2666"
)

type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	Headers    http.Header
	Model      adkmodels.ModelConfig
}

func normaliseBaseURL(value string) string {
	if value == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(value, "/")
}
