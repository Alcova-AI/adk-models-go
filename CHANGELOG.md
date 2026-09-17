# Changelog

## [Unreleased]

### Added

- Optional request timeouts through `LLMRequest.Config.HTTPOptions.Timeout` for the Anthropic, OpenAI and Vercel adapters. The timeout covers retries and stream consumption, and remains unset by default.
- Shared `GenerateWithTimeout` helper and `ErrRequestTimeout` error to distinguish model request timeouts from caller cancellation. The timer stops before the final response is handed to downstream tool execution.

## [v0.1.3] - Provider compatibility

### Fixed

- Prepare complete type alternatives for Gemini through Vercel so nullable arrays and objects do not produce rejected `anyOf` sibling fields. Preserve accepted values and native Vertex's existing schema representation.

- Support optional function-call output IDs in newer OpenAI SDKs, including the compatibility fix from #6.

- Preserve incomplete streamed Anthropic tool input and cumulative token usage with the updated SDK. Read streamed OpenAI tool names from their output items.

### Changed

- Upgrade ADK to v2.4.0, Anthropic to v1.73.0, OpenAI to v3.61.0 and Google GenAI to v1.71.0.
- Upgrade JSON Schema support to v0.4.3 and OAuth2 to v0.37.0.

## [v0.1.2] - Enterprise Web Search

### Added

- Native Vercel support for Google Enterprise Web Search
- Source links and Google grounding metadata in streamed and non-streamed responses

## [v0.1.1] - Tool schema compatibility

### Added

- Shared tool-schema compatibility checks for OpenAI, Anthropic and Vercel adapters, plus `toolschema.WrapGemini` for native Gemini clients.
- Support for acyclic named local references (`#/$defs/name` and `#/definitions/name`) within each tool schema.
- A live compatibility matrix covering schema acceptance, argument preservation and streaming across provider routes.

### Changed

- Unsupported tool-schema constraints now fail by default. Callers can explicitly set `ToolSchemas.AllowUnsupported` to permit documented weaker enforcement with structured warnings. Malformed schemas and unsupported reference forms still fail.
- External, recursive, root and dynamic references, anchors and nested reference scopes are outside the supported reference contract. Claude also rejects references inside `allOf`.
- Provider complexity limits still apply. Callers must validate returned arguments before executing tools.

### Fixed

- Preserve numeric JSON values and nullable enums during schema conversion, with explicit handling of provider-specific alternatives.
- For OpenAI through Vercel Responses, explicit fallback represents omitted optional non-nullable inputs with null markers and removes those markers from returned arguments. Required values, real nulls and zero remain intact; omitted nullable inputs can still return as explicit null.

## [v0.1.0] - Initial combined ADK model adapters

### Added

- Anthropic Messages, OpenAI Responses, and native Vercel AI Gateway adapters in one module
- Shared model-family detection, reasoning mappings, prompt caching configuration, and response metadata
- Canonical model names separate from request routing aliases
- Explicit Vercel gateway routing and retention controls, with validation of conflicting raw options
- Streaming, tool calling, structured output, and preserved opaque reasoning state from the original adapters
- Migration guidance in the README
- Automatic release tag previews, tagging, and GitHub releases for merged pull requests

- Default native Vercel HTTP client retries transient failures twice with cancellable backoff; supplied clients remain unchanged

- Gemini gateway thinking levels use the lowercase encoding required by Vercel

### Changed

- OpenAI minimal reasoning maps to `none` in both Responses and gateway options
- OpenAI response storage is always disabled
- Gateway retention is allowed by default; zero retention must be explicitly requested
- Thinking budgets are rejected in favour of shared reasoning levels
