// Copyright 2026 Alcova AI
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

package adkopenai

import (
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/openai/openai-go/v3"
)

func TestPromptCachingConfig_ProviderDefaultRejectsBreakpoints(t *testing.T) {
	err := (PromptCachingConfig{
		Mode:              PromptCacheProviderDefault,
		SystemInstruction: &CacheBreakpoint{},
	}).Validate()
	if err == nil {
		t.Fatal("validate() error = nil")
	}
}

func TestPromptCachingConfig_RejectsAmbiguousInstructionBoundary(t *testing.T) {
	err := (PromptCachingConfig{
		Mode:              PromptCacheExplicit,
		SystemInstruction: &CacheBreakpoint{},
		Tools:             &CacheBreakpoint{},
	}).Validate()
	if err == nil {
		t.Fatal("validate() error = nil")
	}
}

func TestPromptCachingConfig_AcceptsExplicitBreakpoints(t *testing.T) {
	err := (PromptCachingConfig{
		Mode:                PromptCacheExplicit,
		SystemInstruction:   &CacheBreakpoint{},
		ConversationHistory: &CacheBreakpoint{},
	}).Validate()
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestPromptCachingConfigRejectsGatewayManagedMode(t *testing.T) {
	err := (PromptCachingConfig{
		Mode:              "gateway_automatic",
		SystemInstruction: &CacheBreakpoint{},
	}).Validate()
	if err == nil {
		t.Fatal("validate() error = nil")
	}
}

func TestGatewayAutomaticCachingIsConfiguredOnVercel(t *testing.T) {
	_, err := NewModel(Config{Client: openai.NewClient(), Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna", Vercel: &adkmodels.VercelConfig{Caching: adkmodels.GatewayCachingAuto}}})
	if err != nil {
		t.Fatal(err)
	}
}
