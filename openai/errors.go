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
	"errors"

	converters "github.com/Alcova-AI/adk-models-go/internal/openaiconvert"
)

// Converter errors are re-exported from the root package for callers that need
// stable errors.Is checks without importing the converters package.
var (
	ErrModelNameRequired              = converters.ErrModelNameRequired
	ErrRequestNil                     = converters.ErrRequestNil
	ErrNoContents                     = converters.ErrNoContents
	ErrFunctionCallMissingName        = converters.ErrFunctionCallMissingName
	ErrTopKNotSupported               = converters.ErrTopKNotSupported
	ErrStopSequencesNotSupported      = converters.ErrStopSequencesNotSupported
	ErrMultipleCandidatesNotSupported = converters.ErrMultipleCandidatesNotSupported
	ErrPenaltiesNotSupported          = converters.ErrPenaltiesNotSupported
	ErrLabelsNotSupported             = converters.ErrLabelsNotSupported
	ErrSafetySettingsNotSupported     = converters.ErrSafetySettingsNotSupported
	ErrUnsupportedMIMEType            = converters.ErrUnsupportedMIMEType
	ErrEmptyJSONSchema                = converters.ErrEmptyJSONSchema
	ErrEmptyResponse                  = converters.ErrEmptyResponse
	ErrNoOutputItems                  = converters.ErrNoOutputItems
	ErrUnsupportedMessageContentType  = converters.ErrUnsupportedMessageContentType
	ErrUnsupportedOutputItemType      = converters.ErrUnsupportedOutputItemType
	ErrFunctionCallArgs               = converters.ErrFunctionCallArgs
	ErrNoTextOrToolContent            = converters.ErrNoTextOrToolContent

	// ErrMissingTerminalResponse is returned when a stream ends without a
	// completed or incomplete response event.
	ErrMissingTerminalResponse = errors.New("openai: stream ended without a terminal response")
)
