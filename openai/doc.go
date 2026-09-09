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

// Package adkopenai implements ADK's model.LLM interface on top of the
// OpenAI Responses API.
//
// The caller constructs the OpenAI SDK client, so the same adapter can use
// the direct OpenAI API or a compatible endpoint without the adapter owning
// credentials or endpoint policy. The optional vercel subpackage adds typed
// Vercel AI Gateway routing and response metadata.
package adkopenai
