// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package adkopenai

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	sdk "github.com/openai/openai-go/v3"
)

// Config preserves caller ownership of SDK authentication and transport.
type Config struct {
	Client sdk.Client
	Model  adkmodels.ModelConfig
}
