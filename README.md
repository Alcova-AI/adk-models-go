# ADK Models Go

Anthropic, OpenAI and Vercel model adapters for Google's Agent Development Kit for Go.

`adk-models-go` combines `adk-anthropic-go`, `adk-openai-go` and `adk-vercel-go` into one Go module. It provides shared model-family detection, reasoning mappings, configuration and response metadata while preserving each adapter's request format.

> **Development status:** The initial release is under development. This README describes the approved interface. Installation and migration require a published version.

## Adapters

Each adapter implements ADK's `model.LLM` interface. Choose the request format explicitly.

| Package | Request format | Client |
|---|---|---|
| `anthropic` | Anthropic Messages | Caller-supplied Anthropic SDK client |
| `openai` | OpenAI Responses | Caller-supplied OpenAI SDK client |
| `vercel` | Native Vercel | Optional HTTP client |

Direct Anthropic and OpenAI adapters require their own model family. Cross-family requests require Vercel. Direct Gemini stays on Google ADK's existing Gemini adapter; Gemini through Vercel uses this library.

The library does not switch adapters automatically. Model-family recognition does not guarantee that every endpoint supports that model or its features.

## Installation

After the first release is published:

```bash
go get github.com/Alcova-AI/adk-models-go
```

All adapter packages share one module version.

## Direct OpenAI

Construct the SDK client to control authentication, endpoint selection, HTTP behaviour and SDK options.

```go
import (
    adkmodels "github.com/Alcova-AI/adk-models-go"
    adkopenai "github.com/Alcova-AI/adk-models-go/openai"

    openaisdk "github.com/openai/openai-go/v3"
    "github.com/openai/openai-go/v3/option"
)

client := openaisdk.NewClient(option.WithAPIKey(apiKey))
llm, err := adkopenai.NewModel(adkopenai.Config{
    Client: client,
    Model: adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna"},
})
```

The OpenAI adapter always sends `store: false`. ADK owns conversation history, including the opaque reasoning state needed for later turns.

## Direct Anthropic

```go
import (
    adkmodels "github.com/Alcova-AI/adk-models-go"
    adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"

    anthropicsdk "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

client := anthropicsdk.NewClient(option.WithAPIKey(apiKey))
llm, err := adkanthropic.NewModel(adkanthropic.Config{
    Client: client,
    Model: adkmodels.ModelConfig{CanonicalModel: "claude-sonnet-4-6"},
})
```

## Native Vercel

```go
import (
    adkmodels "github.com/Alcova-AI/adk-models-go"
    adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
)

llm, err := adkvercel.NewModel(adkvercel.Config{
    APIKey: apiKey,
    Model: adkmodels.ModelConfig{
        CanonicalModel: "gpt-5.6-luna",
        RequestModel: "openai/gpt-5.6-luna",
    },
})
```

Native Vercel accepts optional `BaseURL`, `HTTPClient` and `Headers` settings. If no HTTP client is supplied, it creates a dedicated client with two retries for connection failures and HTTP 408, 409, 429 and 5xx responses. It respects `x-should-retry` and `Retry-After` (up to 60 seconds), otherwise using exponential backoff with jitter. Cancellation stops retries. Supplied clients are used unchanged. Errors after a successful response starts streaming are returned without retry.

Anthropic Messages and OpenAI Responses can also be used through Vercel-compatible endpoints. Configure their endpoint through the supplied SDK client and their gateway behaviour through `ModelConfig.Vercel`.

## Model names

| Field | Purpose |
|---|---|
| `CanonicalModel` | Stable model identity used to select the model-family mapping |
| `RequestModel` | Exact identifier sent to the endpoint; defaults to `CanonicalModel` |

| Model family | Recognised canonical patterns |
|---|---|
| OpenAI | `gpt-*`, `o1`, `o1-*`, `o3`, `o3-*`, `o4`, `o4-*` |
| Anthropic | `claude-*` |
| Gemini | `gemini-*` |
| Z.ai | `glm-*` |

Family detection ignores case and surrounding spaces. Canonical names must be unqualified; gateway prefixes belong in `RequestModel`. Unrecognised canonical names are rejected. There is no family override.

For request names, recognised family mismatches are rejected. Unknown endpoint aliases are accepted without a family cross-check. An explicitly supplied request name is not rewritten.

## Reasoning levels

Set reasoning through ADK's `genai.ThinkingConfig`:

```go
thinking := &genai.ThinkingConfig{
    ThinkingLevel: genai.ThinkingLevelHigh,
    IncludeThoughts: true,
}
```

The library also exposes `adkmodels.ThinkingLevelXHigh` and `adkmodels.ThinkingLevelMax`. These use the existing `genai.ThinkingLevel` type and can be used in backend code and model-route configuration.

### Family mappings

The same model-family mapping applies across adapters.

| Requested level | OpenAI effort | Anthropic effort / thinking | Gemini level | Z.ai effort |
|---|---|---|---|---|
| `MINIMAL` | `none` | `low` / disabled | `MINIMAL` | `low` |
| `LOW` | `low` | `low` / adaptive | `LOW` | `low` |
| `MEDIUM` | `medium` | `medium` / adaptive | `MEDIUM` | `high` |
| `HIGH` | `high` | `high` / adaptive | `HIGH` | `max` |
| `XHIGH` | `xhigh` | `xhigh` / adaptive | `HIGH` | `max` |
| `MAX` | `xhigh` | `max` / adaptive | `HIGH` | `max` |

Z.ai thinking remains enabled for explicit levels. If no level is supplied or configured as a caller-selected default, the library leaves it unset and uses the model's default. Explicit reasoning-token budgets are rejected.

Gemini levels keep their GenAI meaning; Vercel provider options encode them in lowercase (`HIGH` becomes `high`).

The library does not maintain model-specific exceptions. If a model or endpoint rejects a mapped setting, its error is returned without retrying at a different level.

When Anthropic tool use is forced, the adapter preserves its existing exception: omit thinking and clear adaptive effort. This applies to Messages fields and gateway provider options without changing the caller's configuration.

### Reasoning summaries

`IncludeThoughts` controls whether reasoning summaries are returned. It does not change reasoning effort. Opaque reasoning state required for later turns is preserved even when summaries are hidden.

## Vercel configuration

All Vercel-specific behaviour belongs in `VercelConfig`, supplied through `ModelConfig.Vercel`. This includes gateway routing, data-retention requirements, gateway-managed caching and gateway-specific provider options.

Retention is allowed by default. Callers can explicitly require zero data retention. The library sends that requirement to Vercel and returns any gateway error; it does not weaken the requirement or maintain a local provider-support catalogue.

OpenAI's `store: false` remains in effect regardless of the gateway retention setting. Raw provider options that conflict with typed settings are rejected.

## Prompt caching

Caching controls are unset by default. Provider-managed caching may still occur.

When explicitly configured:

- Anthropic retains its cache lifetimes and distinct cache boundaries.
- OpenAI retains its cache modes, keys and boundary rules.
- Gateway-managed caching is configured through `VercelConfig`.
- Unsupported optional features, such as cache breakpoints, are ignored.

Conflicting configuration remains an error. The library does not strip settings and retry after a provider rejects a request.

## Response metadata

```go
metadata, ok := adkmodels.MetadataFromResponse(response)
```

Shared metadata includes response identity, cache-write token counts, gateway routing and attempt information, optional cost, and separate raw provider metadata. ADK's standard token-usage fields remain the standard usage interface.

The caller decides what to log or attach to traces. The library does not automatically log returned metadata or add it to traces.

## Errors and retries

Anthropic and OpenAI preserve retries configured through caller-supplied SDK clients. Native Vercel adds two HTTP retries only when no client is supplied, as described above.

The library does not automatically switch request formats, reduce reasoning effort after an error, weaken retention requirements, or remove caching settings and retry. Provider and gateway errors retain their meaning.

## Migrating from the separate adapters

| Previous module | New adapter package |
|---|---|
| `github.com/Alcova-AI/adk-anthropic-go/v3` | `github.com/Alcova-AI/adk-models-go/anthropic` |
| `github.com/Alcova-AI/adk-openai-go` | `github.com/Alcova-AI/adk-models-go/openai` |
| `github.com/Alcova-AI/adk-vercel-go` | `github.com/Alcova-AI/adk-models-go/vercel` |

Migration requires configuration changes, not just new imports:

1. Move shared model settings into `adkmodels.ModelConfig`.
2. Move gateway-specific behaviour into `VercelConfig`.
3. Use the shared reasoning levels and family mappings.
4. Use the shared metadata accessor.
5. Check the new retention default: explicitly require zero data retention where needed.
6. Remove caller-side workarounds that would override the shared mappings.

For the Alcova backend, remove `lunaReasoningModel` and its factory hook from [PR #2322](https://github.com/Alcova-AI/alcova-backend/pull/2322) in the same migration. The shared OpenAI mapping translates `MINIMAL` to `none`; retaining the wrapper would incorrectly change it to `LOW` first.

Existing adapter releases remain available during migration.

## Licence

Apache License 2.0. See [LICENSE](LICENSE). Existing copyright notices and applicable [third-party attribution](THIRD_PARTY_NOTICES.md) are preserved.

## Tool schema compatibility (trial)

Every adapter checks function schemas before sending a request. Define inputs
with either `FunctionDeclaration.Parameters` (`genai.Schema`) or
`ParametersJsonSchema` (a JSON-serialisable schema object), never both. Tool
schemas must have an object root. The raw form uses JSON Schema 2020-12 (other explicitly declared drafts are
rejected during this trial rather than silently reinterpreted);
OpenAPI `nullable` belongs in the typed form, where the adapter converts it to
an `anyOf` null alternative. Property names and example/default/enum data are
not treated as schema keywords.

The default rejects unsupported constraints. Callers that validate arguments
before execution can explicitly permit weaker provider enforcement:

```go
Model: adkmodels.ModelConfig{
    CanonicalModel: "gpt-5.6-luna",
    ToolSchemas: toolschema.Config{AllowUnsupported: true},
}
```

Import `github.com/Alcova-AI/adk-models-go/toolschema`. The optional `Warn`
callback receives the tool name, schema path, keyword, provider, route and
reason. Without a callback, structured warnings use the default Go logger.
Warnings are deduplicated per model instance with a bounded 256-entry cache;
no schema values or arguments are logged. This option never suppresses malformed
schema errors, unknown keywords or unresolved/external references.

For OpenAI and Claude, schemas that meet the checked strict-mode requirements
use `strict: true`. Open objects and, for OpenAI, optional fields that cannot
be represented without changing the contract require the explicit opt-in and
use `strict: false`. In particular, the adapter does not silently make optional
fields required or invent nullability to satisfy OpenAI's automatic normalisation.
Compatibility checking is distinct from provider strict decoding.

The current trial profiles are intentionally conservative:

| Route | Behaviour |
|---|---|
| OpenAI Responses, direct or Vercel | Standard JSON Schema subset; unsupported composition rules require opt-in. Optional fields retain their original meaning. |
| Claude, direct or Vercel | Unsupported numeric bounds, length rules and unrestricted regex patterns require opt-in. Root-level schema rules are preserved. |
| Gemini, direct or Vertex | `toolschema.WrapGemini` checks the input while the Google SDK retains ownership of its native typed format. |
| Gemini through Vercel | Checks known additional losses in the public Google converter, including numeric bounds, pattern and maximum string length. |

Presentation-only property ordering may be dropped with a warning. Unsupported
assertions are removed only with opt-in; unsupported `oneOf` can become `anyOf`
with an explicit exclusivity-loss warning. Local static references are checked,
but reference conversion outside OpenAI and dynamic references remain errors
in this trial rather than being silently erased. This is not a complete
cross-provider JSON Schema implementation or a guarantee of business correctness.
Provider model availability, schema complexity limits and model-specific
restrictions still apply. Validate actual arguments before execution.

Sources for these profiles:

- [OpenAI strict tool calling](https://developers.openai.com/api/docs/guides/function-calling#strict-mode)
- [OpenAI supported schemas](https://developers.openai.com/api/docs/guides/structured-outputs#supported-schemas)
- [Claude schema limitations](https://platform.claude.com/docs/en/build-with-claude/structured-outputs#json-schema-limitations)
- [Google schema reference](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/reference/rest/v1/Schema)
- [Vercel Google converter at the protocol's pinned revision](https://github.com/vercel/ai/blob/fe86f8fb03a08af90b05cb79df66d3230d1e2666/packages/google/src/convert-json-schema-to-openapi-schema.ts)

The gateway's private downstream implementation is not inspected by this
library. Live route tests are required before expanding a compatibility claim.
