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

## Tool schema compatibility

Providers support different parts of JSON Schema. The adapters check your tool
schemas before sending a request and reject rules the selected provider cannot
preserve. This check is enabled by default; no configuration is needed.

Define each tool's inputs using **one** of these fields:

- `Parameters`: a typed `genai.Schema`.
- `ParametersJsonSchema`: a JSON-serialisable JSON Schema 2020-12 object.

The root schema must describe an object. Do not set both fields on the same tool.

### Allowing unsupported rules

If a provider cannot support your schema, the default is to return an error.
You can allow the adapter to remove or weaken unsupported rules:

```go
import "github.com/Alcova-AI/adk-models-go/toolschema"

// In your model configuration:
Model: adkmodels.ModelConfig{
    CanonicalModel: "gpt-5.6-luna",
    ToolSchemas: toolschema.Config{AllowUnsupported: true},
}
```

Use this option only when your application validates tool arguments before
executing them. The provider may return values that break the original rules.
Invalid schemas, unknown keywords and unsupported references still return errors.

The adapter logs a warning for each changed rule. Set `ToolSchemas.Warn` if you
want to handle these warnings yourself. Warnings identify the tool and rule;
they do not include schema values or tool arguments.

### What to expect from each provider

The adapters use strict mode where the provider can preserve the schema.
Some schemas need the opt-in above, such as open objects, certain optional
fields, or rules that a provider does not support. Even with strict mode,
validate arguments before executing a tool.

For OpenAI through Vercel Responses, the opt-in also handles optional,
non-nullable typed fields: the model can return a null marker for an omitted
field, and the adapter removes that marker from the final arguments. Required
fields and fields that already allow null keep their meaning. This also covers
named local references and simple nullable alternatives.

All routes accept acyclic named local references (`#/$defs/name` or
`#/definitions/name`). References stay in the same tool schema. External and
recursive references, anchors and nested reference scopes are rejected;
provider schema complexity limits still apply.

See the [live schema matrix](testdata/schema-matrix/README.md) for tested routes,
results, provider-specific limits and instructions for running the tests.
