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

	"github.com/openai/openai-go/v3/responses"
	"google.golang.org/adk/v2/model"
)

func TestResponseMetadataFromResponse(t *testing.T) {
	resp := &model.LLMResponse{}
	openAIResp := &responses.Response{ID: "resp_123"}
	openAIResp.Usage.InputTokensDetails.CacheWriteTokens = 60

	attachOpenAIResponseMetadata(resp, openAIResp)
	metadata, ok := adkmodels.MetadataFromResponse(resp)
	if !ok {
		t.Fatal("adkmodels.MetadataFromResponse() did not find metadata")
	}
	if metadata.ResponseID != "resp_123" {
		t.Errorf("ResponseID = %q, want %q", metadata.ResponseID, "resp_123")
	}
	if metadata.CacheWriteInputTokens != 60 {
		t.Errorf("CacheWriteInputTokens = %d, want 60", metadata.CacheWriteInputTokens)
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
