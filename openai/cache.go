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
	adkmodels "github.com/Alcova-AI/adk-models-go"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

func applyPromptCaching(params *responses.ResponseNewParams, cfg adkmodels.OpenAIPromptCachingConfig) error {
	if cfg.Key != "" {
		params.PromptCacheKey = param.NewOpt(cfg.Key)
	}
	if cfg.Mode == adkmodels.OpenAIPromptCacheProviderDefault {
		return nil
	}
	params.PromptCacheOptions = responses.ResponseNewParamsPromptCacheOptions{
		Mode: string(cfg.Mode),
		Ttl:  "30m",
	}
	availableBreakpoints := 4
	if cfg.Mode == adkmodels.OpenAIPromptCacheImplicit {
		availableBreakpoints--
	}
	if (cfg.SystemInstruction != nil || cfg.Tools != nil) && markInstructionBreakpoint(params.Input.OfInputItemList) {
		availableBreakpoints--
	}
	if cfg.ConversationHistory != nil {
		markHistoryBreakpoints(params.Input.OfInputItemList, availableBreakpoints)
	}
	return nil
}

func markInstructionBreakpoint(items responses.ResponseInputParam) bool {
	for i := range items {
		message := items[i].OfMessage
		if message == nil || message.Role != responses.EasyInputMessageRoleDeveloper {
			continue
		}
		return markLastInputContent(message.Content.OfInputItemContentList)
	}
	return false
}

// markHistoryBreakpoints retains a rolling set of user-message boundaries.
// OpenAI explicit breakpoints are valid on input content but not on assistant
// output_text blocks. Retaining older user boundaries lets a growing request
// read the prior prefix before writing a newer one that includes the latest
// assistant response.
func markHistoryBreakpoints(items responses.ResponseInputParam, limit int) int {
	marked := 0
	for i := len(items) - 1; i >= 0; i-- {
		if marked == limit {
			break
		}
		message := items[i].OfMessage
		if message == nil || message.Role != responses.EasyInputMessageRoleUser || hasMedia(message.Content.OfInputItemContentList) {
			continue
		}
		if markLastInputContent(message.Content.OfInputItemContentList) {
			marked++
		}
	}
	return marked
}

func hasMedia(contents responses.ResponseInputMessageContentListParam) bool {
	for _, content := range contents {
		if content.OfInputImage != nil || content.OfInputFile != nil {
			return true
		}
	}
	return false
}

func markLastInputContent(contents responses.ResponseInputMessageContentListParam) bool {
	for i := len(contents) - 1; i >= 0; i-- {
		switch {
		case contents[i].OfInputText != nil:
			contents[i].OfInputText.PromptCacheBreakpoint = responses.NewResponseInputTextPromptCacheBreakpointParam()
			return true
		case contents[i].OfInputImage != nil:
			contents[i].OfInputImage.PromptCacheBreakpoint = responses.NewResponseInputImagePromptCacheBreakpointParam()
			return true
		case contents[i].OfInputFile != nil:
			contents[i].OfInputFile.PromptCacheBreakpoint = responses.NewResponseInputFilePromptCacheBreakpointParam()
			return true
		}
	}
	return false
}
