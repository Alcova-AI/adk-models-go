// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
)

// Config preserves caller ownership of SDK authentication and transport.
type Config struct {
	// API defaults to Responses when omitted.
	API    API
	Client sdk.Client
	Model  adkmodels.ModelConfig
}

// API selects the OpenAI wire protocol independently of the client endpoint.
type API string

const (
	APIResponses       API = "responses"
	APIChatCompletions API = "chat-completions"
)
