# Changelog

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
