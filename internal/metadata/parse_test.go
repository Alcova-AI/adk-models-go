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

import "testing"

func TestParse(t *testing.T) {
	raw := `{"providerMetadata":{"gateway":{"cost":0.42,"generationId":"gen_1","routing":{"originalModelId":"openai/gpt","resolvedProvider":"azure","canonicalSlug":"openai/gpt","modelAttemptCount":2,"totalProviderAttemptCount":3}}}}`
	metadata, ok := Parse(raw)
	if !ok {
		t.Fatal("Parse() did not find metadata")
	}
	if metadata.GenerationID != "gen_1" || metadata.ResolvedProvider != "azure" || metadata.CostUSD == nil || *metadata.CostUSD != 0.42 {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestParseMetadata_ResponseEnvelope(t *testing.T) {
	raw := `{"response":{"provider_metadata":{"gateway":{"generationId":"gen_response","routing":{"resolvedProvider":"openai"}}}}}`
	metadata, ok := Parse(raw)
	if !ok {
		t.Fatal("Parse() did not find response metadata")
	}
	if metadata.GenerationID != "gen_response" || metadata.ResolvedProvider != "openai" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestParseMetadata_IgnoresUnknownOrMalformedData(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{not json}`} {
		if metadata, ok := Parse(raw); ok {
			t.Fatalf("Parse(%q) = %+v, true", raw, metadata)
		}
	}
}

func TestRawMetadataDoesNotHideNestedGatewayFacts(t *testing.T) {
	raw := `{"providerMetadata":{"custom":{"unknown":42}},"response":{"provider_metadata":{"gateway":{"generationId":"gen_nested","cost":"0.125"},"custom":{"nested":true}}}}`
	got, ok := Parse(raw)
	if !ok || got.GenerationID != "gen_nested" || got.CostUSD == nil || *got.CostUSD != 0.125 {
		t.Fatalf("gateway metadata: %+v", got)
	}
	custom := got.ProviderMetadata["custom"].(map[string]any)
	if custom["unknown"] != float64(42) || custom["nested"] != true {
		t.Fatalf("raw metadata lost: %#v", custom)
	}
}
