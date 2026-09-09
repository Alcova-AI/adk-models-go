// Copyright 2025 Alcova AI
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"encoding/json"
	"strconv"

	adkmodels "github.com/Alcova-AI/adk-models-go"
	"google.golang.org/adk/v2/model"
)

// Parse accepts both compatibility envelopes and streaming response wrappers.
// Unknown provider fields survive alongside the common gateway facts.
func Parse(raw string) (adkmodels.Metadata, bool) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &envelope) != nil {
		return adkmodels.Metadata{}, false
	}
	containers := []map[string]json.RawMessage{envelope}
	for _, key := range []string{"message", "response"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(envelope[key], &nested) == nil {
			containers = append(containers, nested)
		}
	}
	providers := make(map[string]any)
	for _, container := range containers {
		for _, key := range []string{"providerMetadata", "provider_metadata"} {
			var candidate map[string]any
			if json.Unmarshal(container[key], &candidate) == nil {
				mergeMissing(providers, candidate)
			}
		}
	}
	if len(providers) == 0 {
		return adkmodels.Metadata{}, false
	}
	return FromProviders(providers), true
}

// Outer envelopes retain precedence, while nested envelopes may add fields.
func mergeMissing(target, source map[string]any) {
	for key, value := range source {
		existing, found := target[key]
		if !found {
			target[key] = value
			continue
		}
		targetMap, targetOK := existing.(map[string]any)
		sourceMap, sourceOK := value.(map[string]any)
		if targetOK && sourceOK {
			mergeMissing(targetMap, sourceMap)
		}
	}
}

func FromProviders(providers map[string]any) adkmodels.Metadata {
	result := adkmodels.Metadata{ProviderMetadata: providers}
	raw, err := json.Marshal(providers["gateway"])
	if err != nil {
		return result
	}
	var gateway gatewayMetadata
	if json.Unmarshal(raw, &gateway) != nil {
		return result
	}
	result.GenerationID = gateway.GenerationID
	result.ResolvedProvider = gateway.Routing.ResolvedProvider
	result.OriginalModelID = gateway.Routing.OriginalModelID
	result.CanonicalModel = gateway.Routing.CanonicalSlug
	result.ModelAttemptCount = gateway.Routing.ModelAttemptCount
	result.ProviderAttemptCount = gateway.Routing.TotalProviderAttemptCount
	result.CostUSD = parseCost(gateway.Cost)
	return result
}

func Attach(response *model.LLMResponse, value adkmodels.Metadata) {
	if response == nil {
		return
	}
	if response.CustomMetadata == nil {
		response.CustomMetadata = make(map[string]any)
	}
	response.CustomMetadata[adkmodels.MetadataKey] = value
}

// AttachGateway preserves the adapter's response ID and usage while adding
// gateway facts from the provider envelope.
func AttachGateway(response *model.LLMResponse, gateway adkmodels.Metadata) {
	current, _ := adkmodels.MetadataFromResponse(response)
	gateway.ResponseID = current.ResponseID
	gateway.CacheWriteInputTokens = current.CacheWriteInputTokens
	gateway.CacheCreation5mInputTokens = current.CacheCreation5mInputTokens
	gateway.CacheCreation1hInputTokens = current.CacheCreation1hInputTokens
	Attach(response, gateway)
}

type gatewayMetadata struct {
	GenerationID string          `json:"generationId"`
	Cost         json.RawMessage `json:"cost"`
	Routing      struct {
		OriginalModelID           string `json:"originalModelId"`
		ResolvedProvider          string `json:"resolvedProvider"`
		CanonicalSlug             string `json:"canonicalSlug"`
		ModelAttemptCount         int    `json:"modelAttemptCount"`
		TotalProviderAttemptCount int    `json:"totalProviderAttemptCount"`
	} `json:"routing"`
}

func parseCost(raw json.RawMessage) *float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return &number
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	return &number
}
