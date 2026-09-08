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

package converters

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3/responses"
)

var reasoningSignaturePrefix = []byte("openai.responses.reasoning.v1:")

func encodeReasoningSignature(item responses.ResponseOutputItemUnion) ([]byte, error) {
	if item.Type != "reasoning" {
		return nil, fmt.Errorf("openai: cannot encode %q as reasoning state", item.Type)
	}
	raw := item.RawJSON()
	if raw == "" {
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("openai: encode reasoning state: %w", err)
		}
		raw = string(encoded)
	}
	signature := make([]byte, 0, len(reasoningSignaturePrefix)+len(raw))
	signature = append(signature, reasoningSignaturePrefix...)
	signature = append(signature, raw...)
	return signature, nil
}

func decodeReasoningSignature(signature []byte) (*responses.ResponseReasoningItemParam, bool, error) {
	if !bytes.HasPrefix(signature, reasoningSignaturePrefix) {
		return nil, false, nil
	}
	raw := signature[len(reasoningSignaturePrefix):]
	var item responses.ResponseReasoningItemParam
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, true, fmt.Errorf("openai: decode reasoning state: %w", err)
	}
	return &item, true, nil
}
