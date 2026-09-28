<a name="readme-top"></a>

<div align="center">
  <a href="https://github.com/Alcova-AI/adk-models-go/blob/main/LICENSE"><img src="https://img.shields.io/github/license/Alcova-AI/adk-models-go" alt="Licence"></a>
  <a href="https://pkg.go.dev/github.com/Alcova-AI/adk-models-go"><img src="https://pkg.go.dev/badge/github.com/Alcova-AI/adk-models-go.svg" alt="Go Reference"></a>
  <a href="https://github.com/Alcova-AI/adk-models-go/releases"><img src="https://img.shields.io/github/v/release/Alcova-AI/adk-models-go" alt="Release"></a>
  <a href="https://github.com/Alcova-AI/adk-models-go/actions/workflows/test.yml"><img src="https://github.com/Alcova-AI/adk-models-go/actions/workflows/test.yml/badge.svg" alt="Test"></a>
</div>

---

# ADK Models Go

**Run Claude, GPT, Gemini and GLM models inside Google's [Agent Development Kit for Go](https://github.com/google/adk-go).** One Go module with Anthropic, OpenAI and Vercel AI Gateway adapters that share model detection, reasoning levels, configuration and response metadata. Open source under Apache 2.0.

---

## Why ADK Models Go?

- **One module, three adapters**: `anthropic`, `openai` and `vercel` packages share one version and one `ModelConfig`. This replaces the separate `adk-anthropic-go`, `adk-openai-go` and `adk-vercel-go` modules.
- **One reasoning scale for every model**: set `genai.ThinkingLevel` once. The adapters map it to each family's native setting (see [family mappings](#family-mappings)).
- **You own the client**: pass your own Anthropic or OpenAI SDK client, so authentication, endpoints, retries and HTTP behaviour stay under your control.
- **Multiple model families through Vercel**: route across providers with typed routing, zero-data-retention and caching settings on `VercelConfig`.
- **Tool schemas checked before sending**: each adapter rejects JSON Schema rules the provider cannot keep, instead of letting the provider drop them without notice. See the [live schema matrix](testdata/schema-matrix/README.md).
- **No hidden fallbacks**: the library never switches request format, lowers reasoning effort, weakens retention, or strips caching and retries. Provider errors reach you unchanged.

---

## Feature overview

| Feature | Description |
|---------|-------------|
| [**Direct Anthropic**](#direct-anthropic) | Claude through the Anthropic Messages API, with your SDK client |
| [**Direct OpenAI**](#direct-openai) | GPT and o-series through Responses or Chat Completions, with your SDK client |
| [**Chat Completions**](#chat-completions) | Text, tools and supported media through OpenAI-compatible endpoints |
| [**Vercel AI Gateway**](#vercel-ai-gateway) | Any supported model family through Vercel's native protocol |
| [**Reasoning levels**](#reasoning-levels) | One thinking scale mapped to each model family |
| [**Prompt caching**](#prompt-caching) | Anthropic breakpoints, OpenAI cache modes and gateway-managed caching |
| [**Response metadata**](#response-metadata) | Response IDs, cache-write tokens, gateway routing and cost |
| [**Tool schema compatibility**](#tool-schema-compatibility) | Checks tool schemas against each provider before sending |
| [**Request timeouts**](#request-timeouts) | Optional per-request timeout that covers retries and streaming |

---

## Quick start

Install the module. All adapter packages share one version.

```bash
go get github.com/Alcova-AI/adk-models-go
```

Every adapter returns an ADK `model.LLM`. Pass it to any ADK agent:

```go
agent, err := llmagent.New(llmagent.Config{
    Name:        "assistant",
    Model:       llm, // from any adapter below
    Instruction: "You are a helpful assistant.",
})
```

### Direct Anthropic

Use Claude models through the Anthropic Messages API.

```go
import (
    adkmodels "github.com/Alcova-AI/adk-models-go"
    adkanthropic "github.com/Alcova-AI/adk-models-go/anthropic"

    anthropicsdk "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

client := anthropicsdk.NewClient(option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))

llm, err := adkanthropic.NewModel(adkanthropic.Config{
    Client: client,
    Model:  adkmodels.ModelConfig{CanonicalModel: "claude-sonnet-4-6"},
})
```

### Direct OpenAI

Use OpenAI models through the Responses API by default. For Chat Completions, set `API: adkopenai.APIChatCompletions`; see [supported features and limits](#chat-completions).

```go
import (
    adkmodels "github.com/Alcova-AI/adk-models-go"
    adkopenai "github.com/Alcova-AI/adk-models-go/openai"

    openaisdk "github.com/openai/openai-go/v3"
    "github.com/openai/openai-go/v3/option"
)

client := openaisdk.NewClient(option.WithAPIKey(os.Getenv("OPENAI_API_KEY")))

llm, err := adkopenai.NewModel(adkopenai.Config{
    Client: client,
    Model:  adkmodels.ModelConfig{CanonicalModel: "gpt-5.6-luna"},
})
```

The OpenAI adapter sends `store: false`. ADK owns the conversation history. Responses preserves opaque reasoning state needed for later turns; Chat Completions rejects reasoning history.

### Vercel AI Gateway

Use any supported model family through one gateway, with routing and retention controls.

```go
import (
    adkmodels "github.com/Alcova-AI/adk-models-go"
    adkvercel "github.com/Alcova-AI/adk-models-go/vercel"
)

llm, err := adkvercel.NewModel(adkvercel.Config{
    APIKey: os.Getenv("AI_GATEWAY_API_KEY"),
    Model: adkmodels.ModelConfig{
        CanonicalModel: "gpt-5.6-luna",
        RequestModel:   "openai/gpt-5.6-luna",
        Vercel: &adkmodels.VercelConfig{
            ZeroDataRetention: true,
            Sort:              adkmodels.GatewaySortTTFT,
        },
    },
})
```

The native Vercel adapter also accepts optional `BaseURL`, `HTTPClient` and `Headers`. It supports Google Enterprise Web Search through `genai.Tool.EnterpriseWebSearch` and returns source links as grounding metadata.

---

## Choosing an adapter

Each adapter implements ADK's `model.LLM` interface. Choose the request format explicitly. The library does not switch adapters for you.

| Package | Request format | Client | Model families |
|---|---|---|---|
| `anthropic` | Anthropic Messages | Your Anthropic SDK client | Anthropic |
| `openai` | OpenAI Responses (default) or Chat Completions | Your OpenAI SDK client | OpenAI |
| `vercel` | Native Vercel | Optional HTTP client | Any recognised family |

- For direct Gemini, use Google ADK's own Gemini adapter. For Gemini through Vercel, use this library.
- To call a model from a different family, use the `vercel` adapter.
- You can also point the `anthropic` and `openai` adapters at Vercel-compatible endpoints. Set the endpoint on your SDK client and the gateway behaviour in `ModelConfig.Vercel`. Both OpenAI API modes support this route.

A recognised model family does not guarantee that an endpoint supports that model or all its features.

### Model names

| Field | Purpose |
|---|---|
| `CanonicalModel` | Stable model identity. Selects the model-family mapping. |
| `RequestModel` | Exact identifier sent to the endpoint. Defaults to `CanonicalModel`. |

| Model family | Recognised canonical patterns |
|---|---|
| OpenAI | `gpt-*`, `o1`, `o1-*`, `o3`, `o3-*`, `o4`, `o4-*` |
| Anthropic | `claude-*` |
| Gemini | `gemini-*` |
| Z.ai | `glm-*` |

- Family detection ignores case and surrounding spaces.
- Canonical names must not have a gateway prefix. Put prefixes such as `openai/` in `RequestModel`.
- The library rejects unrecognised canonical names. There is no family override.
- The library rejects a request name from a different recognised family. It accepts unknown endpoint aliases without a family check.
- The library never rewrites a request name you supply.

---

## Chat Completions

Set `API: adkopenai.APIChatCompletions` on the OpenAI adapter. Omit `API`, or use
`adkopenai.APIResponses`, for Responses. Unknown API values return an error.
The SDK client still owns authentication and endpoint selection.

```go
llm, err := adkopenai.NewModel(adkopenai.Config{
    Client: client,
    API:    adkopenai.APIChatCompletions,
    Model: adkmodels.ModelConfig{
        CanonicalModel: "gpt-6-luna",
        Reasoning: adkmodels.ReasoningConfig{
            DefaultLevel: genai.ThinkingLevelMinimal, // OpenAI effort "none".
        },
    },
})
```

For Vercel, set the SDK client's base URL to `https://ai-gateway.vercel.sh/v1`,
use a qualified `RequestModel`, and supply `ModelConfig.Vercel`. Gateway routing,
retention and provider reasoning options remain available.

- Supports streamed and non-streamed text, function calls, text tool results,
  tool selection, JSON/schema output, user images, inline files and uploaded
  non-image file IDs. Final responses include token usage and response metadata.
  Individual features still depend on the endpoint.
- Rejects reasoning summaries and history, reasoning context/mode controls,
  explicit cache modes/breakpoints, log probabilities, multiple candidates,
  top-K, labels and safety settings. OpenAI models support cache keys only.
- Rejects media in tool results, file URLs and image file IDs.
- Direct GPT-6 Luna/Sol tool calls require `MINIMAL` (effort `none`). Direct
  GPT-6 Astra tool calls require Responses. These checks cover known dated
  variants and do not apply when tools are disabled or the route uses Vercel.
  Other model restrictions are left to the endpoint.

The [opt-in live test matrix](openai/chat_live_test.go) covers direct OpenAI and
Vercel. Run `ADK_CHAT_LIVE=1 go test ./openai -run '^TestChatLiveMatrix$' -count=1 -v`
with the required `OPENAI_API_KEY` and/or `AI_GATEWAY_API_KEY`. Select a route with
`ADK_CHAT_LIVE_ROUTE=direct`, `vercel-openai` or `vercel-google`. The suite disables
retries and reserves at most USD 0.10 per run at its documented fixture rates.

---

## Reasoning levels

Set reasoning through ADK's `genai.ThinkingConfig`:

```go
thinking := &genai.ThinkingConfig{
    ThinkingLevel:   genai.ThinkingLevelHigh,
    IncludeThoughts: true,
}
```

The library adds `adkmodels.ThinkingLevelXHigh` and `adkmodels.ThinkingLevelMax`. They use the existing `genai.ThinkingLevel` type, so you can use them in backend code and model-route configuration. Set a route default with `ModelConfig.Reasoning.DefaultLevel`.

### Family mappings

The same mapping applies across all adapters.

| Requested level | OpenAI effort | Anthropic effort / thinking | Gemini level | Z.ai effort |
|---|---|---|---|---|
| `MINIMAL` | `none` | `low` / disabled | `MINIMAL` | `low` |
| `LOW` | `low` | `low` / adaptive | `LOW` | `low` |
| `MEDIUM` | `medium` | `medium` / adaptive | `MEDIUM` | `high` |
| `HIGH` | `high` | `high` / adaptive | `HIGH` | `max` |
| `XHIGH` | `xhigh` | `xhigh` / adaptive | `HIGH` | `max` |
| `MAX` | `xhigh` | `max` / adaptive | `HIGH` | `max` |

- Z.ai thinking stays on for explicit levels. With no level set, the library leaves it unset and the model uses its default.
- The library rejects explicit reasoning-token budgets.
- Gemini levels keep their GenAI meaning. Vercel provider options send them in lowercase (`HIGH` becomes `high`).
- Apart from the [direct Chat Completions tool restrictions](#chat-completions), model-specific support is left to the endpoint. Provider errors are returned without retrying at a different level.
- When Anthropic tool use is forced, the adapter omits thinking and clears adaptive effort for that request. This applies to Messages fields and gateway provider options. Your configuration does not change.

### Reasoning summaries

`IncludeThoughts` controls whether reasoning summaries are returned. It does not change reasoning effort. Responses, Anthropic and native Vercel keep the opaque reasoning state that later turns need, even when summaries are hidden. Chat Completions rejects reasoning summaries and history.

---

## Vercel configuration

Put all Vercel-specific behaviour in `VercelConfig` and supply it through `ModelConfig.Vercel`. This covers gateway routing, data retention, gateway-managed caching and gateway provider options.

- Retention is allowed by default. Set `ZeroDataRetention: true` to require zero data retention.
- The library sends that requirement to Vercel and returns any gateway error. It never weakens the requirement and keeps no local list of which providers support it.
- OpenAI's `store: false` applies whatever the gateway retention setting is.
- The library rejects raw provider options that conflict with typed settings.

---

## Prompt caching

Caching controls are unset by default. Providers may still cache on their own.

Set caching through `ModelConfig.PromptCaching`:

- **Anthropic**: set `Anthropic.Mode` to `AnthropicPromptCacheManual`, then set breakpoints with their lifetimes.
- **OpenAI Responses**: set `OpenAI.Mode` to implicit or explicit, with an optional cache key and breakpoints.
- **OpenAI Chat Completions**: supports an optional cache key for OpenAI models; explicit cache modes and breakpoints return an error.
- **Vercel**: set `VercelConfig.Caching` to `GatewayCachingAuto` for gateway-managed caching.

Cache support depends on the adapter and model family. Chat Completions rejects the unsupported controls listed above. Conflicting configuration is an error. The library does not strip settings and retry after a provider rejects a request.

---

## Response metadata

```go
metadata, ok := adkmodels.MetadataFromResponse(response)
if ok && metadata.CostUSD != nil {
    fmt.Println(metadata.ResolvedProvider, *metadata.CostUSD)
}
```

`Metadata` includes the response ID, cache-write token counts, gateway routing and attempt counts, optional cost, and the raw provider metadata. ADK's standard token-usage fields stay the main usage interface.

The library does not log metadata or add it to traces. You decide what to record.

---

## Tool schema compatibility

Providers support different parts of JSON Schema. The adapters check your tool schemas before they send a request. They reject rules that the selected provider cannot keep. This check is on by default and needs no configuration.

Define each tool's inputs with **one** of these fields:

- `Parameters`: a typed `genai.Schema`.
- `ParametersJsonSchema`: a JSON-serialisable JSON Schema 2020-12 object.

The root schema must describe an object. Do not set both fields on the same tool.

### Allowing unsupported rules

By default, the adapter returns an error when a provider cannot support your schema. To let the adapter remove or weaken unsupported rules instead:

```go
import "github.com/Alcova-AI/adk-models-go/toolschema"

Model: adkmodels.ModelConfig{
    CanonicalModel: "gpt-5.6-luna",
    ToolSchemas:    toolschema.Config{AllowUnsupported: true},
}
```

Use this option only when your application validates tool arguments before it runs a tool. The provider may return values that break the original rules. Invalid schemas, unknown keywords and unsupported references still return errors.

The adapter logs a warning for each changed rule. Set `ToolSchemas.Warn` to handle warnings yourself. Warnings name the tool and the rule. They do not include schema values or tool arguments.

### What to expect from each provider

- The adapters use strict mode where the provider can keep the schema. Even with strict mode, validate arguments before you run a tool.
- Some schemas need `AllowUnsupported`, such as open objects, some optional fields, or rules a provider does not support.
- For OpenAI through Vercel Responses, `AllowUnsupported` also handles optional, non-nullable typed fields. The model can return a null marker for an omitted field, and the adapter removes that marker from the final arguments. Required fields and fields that already allow null keep their meaning. This also covers named local references and simple nullable alternatives.
- All routes accept acyclic named local references (`#/$defs/name` or `#/definitions/name`). The adapters reject external and recursive references, anchors and nested reference scopes. Provider schema complexity limits still apply.
- Gemini through Vercel receives complete alternatives for type lists such as `["array", "null"]`. Item rules and descriptions stay inside each alternative. Descriptions on explicit alternatives move into their branches. Unsupported combinations of `anyOf` with other assertions stay errors, even with `AllowUnsupported`, so no assertion is lost without notice. Native Vertex keeps its existing JSON Schema representation.

See the [live schema matrix](testdata/schema-matrix/README.md) for tested routes, results, provider limits and how to run the tests.

---

## Request timeouts

The adapters honour `LLMRequest.Config.HTTPOptions.Timeout` when it is positive. When it is unset or not positive, the library adds no timeout. Your context deadlines and SDK client timeouts still apply.

- The timeout covers retries and the full response stream.
- It starts when you begin to iterate and ends when iteration ends or stops early.
- The stream closes before the final non-partial response, so tool execution after it does not count towards the model timeout.
- Use `errors.Is(err, context.DeadlineExceeded)` to detect a timeout. Check your own context to tell your deadline apart from the request timeout. Caller cancellation stays `context.Canceled`.

---

## Errors and retries

- The Anthropic and OpenAI adapters keep the retries you configure on your SDK client.
- The native Vercel adapter adds two HTTP retries only when you do not supply an HTTP client. It retries connection failures and HTTP 408, 409, 429 and 5xx responses.
- It respects `x-should-retry` and `Retry-After` (up to 60 seconds). Otherwise it uses exponential backoff with jitter.
- Cancellation stops retries. A supplied HTTP client is used unchanged.
- Native Vercel and OpenAI return streaming errors without a retry. The Anthropic adapter can retry an overload before it has emitted any response; it does not replay output already delivered to the caller.

---

## Migrating from the separate adapters

| Previous module | New package |
|---|---|
| `github.com/Alcova-AI/adk-anthropic-go/v3` | `github.com/Alcova-AI/adk-models-go/anthropic` |
| `github.com/Alcova-AI/adk-openai-go` | `github.com/Alcova-AI/adk-models-go/openai` |
| `github.com/Alcova-AI/adk-vercel-go` | `github.com/Alcova-AI/adk-models-go/vercel` |

Migration needs configuration changes, not only new imports:

1. Move shared model settings into `adkmodels.ModelConfig`.
2. Move gateway-specific behaviour into `VercelConfig`.
3. Use the shared reasoning levels and family mappings.
4. Use `adkmodels.MetadataFromResponse` for response metadata.
5. Check the new retention default. Set `ZeroDataRetention: true` where you need it.
6. Remove caller-side workarounds that override the shared mappings. For example, the OpenAI mapping sends `MINIMAL` as `none`. A wrapper that changes `MINIMAL` to `LOW` first now gives the wrong result.

Existing releases of the separate adapters stay available while you migrate.

---

## Resources

- [Go package reference](https://pkg.go.dev/github.com/Alcova-AI/adk-models-go)
- [Changelog](CHANGELOG.md)
- [Live schema matrix](testdata/schema-matrix/README.md)
- [Google ADK for Go](https://github.com/google/adk-go)

---

## Contributing

Bug reports and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for development checks, live-test guidance and contribution terms.

---

## Licence

Licensed under the [Apache License 2.0](LICENSE). Existing copyright notices and [third-party attributions](THIRD_PARTY_NOTICES.md) are preserved.

<p align="right"><a href="#readme-top">↑ Back to top</a></p>
