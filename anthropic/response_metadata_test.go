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

package adkanthropic

import (
	"testing"

	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/adk/v2/model"
)

func TestResponseMetadataFromResponse(t *testing.T) {
	resp := &model.LLMResponse{}
	msg := &anthropic.Message{
		ID: "msg_123",
		Usage: anthropic.Usage{
			CacheCreationInputTokens: 60,
			CacheCreation: anthropic.CacheCreation{
				Ephemeral5mInputTokens: 40,
				Ephemeral1hInputTokens: 20,
			},
		},
	}

	attachAnthropicResponseMetadata(resp, msg)
	metadata, ok := adkmodels.MetadataFromResponse(resp)
	if !ok {
		t.Fatal("adkmodels.MetadataFromResponse() did not find metadata")
	}
	if metadata.ResponseID != "msg_123" {
		t.Errorf("MessageID = %q, want %q", metadata.ResponseID, "msg_123")
	}
	if metadata.CacheWriteInputTokens != 60 || metadata.CacheCreation5mInputTokens != 40 || metadata.CacheCreation1hInputTokens != 20 {
		t.Errorf("cache metadata = %+v, want total=60 5m=40 1h=20", metadata)
	}
}

func TestResponseMetadataFromResponseMissing(t *testing.T) {
	if _, ok := adkmodels.MetadataFromResponse(nil); ok {
		t.Fatal("adkmodels.MetadataFromResponse(nil) found metadata")
	}
	if _, ok := adkmodels.MetadataFromResponse(&model.LLMResponse{}); ok {
		t.Fatal("adkmodels.MetadataFromResponse(empty) found metadata")
	}
}
