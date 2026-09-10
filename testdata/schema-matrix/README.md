# Live tool schema matrix

Last tested: **10 September 2026**.

The normal adapters were tested on ten routes with **41 non-streaming cases** and **14 streaming cases**, including four inline/reference pairs. Each case requests valid arguments and deliberately conflicting arguments. No tools execute. These are synthetic observations, not production success rates or guaranteed enforcement.

| Run | Records including local blocks | HTTP attempts | Exact valid samples | Valid samples changed |
|---|---:|---:|---:|---:|
| Non-streaming | 1,304 | 820 | 406/410 | 4 |
| Streaming | 418 | 280 | 138/140 | 2 |

No HTTP request was rejected in these collections. Four conflicting streaming samples failed while producing or parsing tool arguments. The default rejects unsupported rules locally; fallback explicitly allows weaker enforcement and runs only when default rejects the schema. The tables below use fallback where necessary. Local blocks make no HTTP request. These runs use normal adapter paths, not provider probes.

## Observed results

**S**: both samples conformed and the valid sample matched exactly. **W**: a sample broke the schema or changed requested arguments. **E**: a call failed. Each cell is **non-streaming / streaming**; a dash means streaming was not selected. These classifications report samples, not a promise that a provider enforces the rule.

| Rule | gemini-messages | gemini-native | gemini-responses | gemini-vertex | haiku-messages | haiku-native | haiku-vertex | luna-direct | luna-native | luna-responses |
|---|---|---|---|---|---|---|---|---|---|---|
| allOf | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| anyOf | S / - | S / - | S / - | S / - | W / - | W / - | W / - | S / - | S / - | S / - |
| closed-object | S / - | S / - | S / - | S / - | S / - | S / - | S / - | S / - | S / - | S / - |
| const | S / - | S / - | S / - | W / - | W / - | S / - | S / - | W / - | W / - | W / - |
| enum | S / - | S / - | S / - | S / - | W / - | S / - | S / - | S / - | S / - | S / - |
| exclusiveMaximum | W / - | W / - | W / - | W / - | W / - | W / - | W / - | S / - | S / - | S / - |
| exclusiveMinimum | W / - | W / - | W / - | W / - | W / - | W / - | W / - | S / - | S / - | S / - |
| format-date | S / - | S / - | S / - | S / - | W / - | S / - | S / - | S / - | S / - | S / - |
| integer | S / - | S / - | S / - | S / - | W / - | S / - | S / - | S / - | S / - | S / - |
| legacy-list-clients-typed | S / S | S / S | S / S | S / S | W / W | W / W | W / W | W / W | W / W | S / S |
| local-ref | S / - | S / - | S / - | S / - | W / - | S / - | S / - | S / - | S / - | S / - |
| maxItems | W / - | S / - | W / - | S / - | W / - | W / - | W / - | S / - | S / - | S / - |
| maxLength | W / - | W / - | W / - | S / - | W / - | W / - | W / - | S / - | S / - | S / - |
| maxProperties | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| maximum | W / - | W / - | W / - | S / - | W / - | W / - | W / - | S / - | S / - | S / - |
| minItems-one | W / W | S / S | W / W | S / S | W / W | S / S | S / S | S / S | S / S | S / S |
| minItems-two | W / - | S / - | W / - | S / - | W / - | W / - | W / - | S / - | S / - | S / - |
| minItems-two-typed | W / - | S / - | W / - | S / - | W / - | W / - | W / - | W / - | W / - | S / - |
| minLength | W / - | W / - | W / - | W / - | W / - | W / - | W / - | S / - | S / - | S / - |
| minProperties | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| minimum | W / W | W / W | W / W | S / S | W / W | W / W | W / W | S / S | S / S | S / S |
| multipleOf | W / - | W / - | W / - | W / - | W / - | W / - | W / - | S / - | S / - | S / - |
| nullable-enum | S / - | S / - | S / - | S / - | W / - | W / - | W / - | S / - | S / - | S / - |
| nullable-enum-typed | S / S | S / S | S / S | S / S | W / W | W / W | W / W | W / W | W / W | S / S |
| nullable-object-inline | S / S | S / S | S / S | S / S | W / W | S / S | S / S | S / S | S / S | S / S |
| nullable-object-ref | S / S | S / S | S / S | S / S | W / W | S / S | S / S | S / S | S / S | S / S |
| oneOf | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| optional-nonnullable-object-inline | S / S | S / S | S / S | S / S | W / W | S / S | S / S | W / W | W / W | S / S |
| optional-nonnullable-object-ref | S / S | S / S | S / S | S / S | W / W | S / S | S / S | W / W | W / W | S / S |
| optional-object-inline | S / S | S / S | S / S | S / S | W / W | S / S | S / S | W / W | W / W | W / W |
| optional-object-ref | S / S | S / S | S / S | S / S | W / W | S / S | S / S | W / W | W / W | W / W |
| optional-pagination | S / S | S / S | S / S | S / S | W / E | W / E | W / W | W / W | W / W | S / S |
| optional-pagination-typed | S / S | S / S | S / S | S / S | W / E | W / E | W / W | W / W | W / W | S / S |
| pagination-not | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| pattern | W / - | W / - | W / - | W / - | W / - | W / - | W / - | S / - | S / - | S / - |
| repeated-object-inline | S / S | S / S | S / S | S / S | W / W | S / S | S / S | S / S | S / S | S / S |
| repeated-object-ref | S / S | S / S | S / S | S / S | W / W | W / S | W / S | S / S | S / S | S / S |
| required | S / - | S / - | S / - | S / - | W / - | S / - | S / - | S / - | S / - | S / - |
| root-anyOf | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| root-oneOf | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |
| uniqueItems | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - | W / - |

## Reference support and limits

The four paired fixtures cover repeated nested objects, explicit null, omission of an optional nullable object, and omission of an optional non-nullable object. References span two definition levels, direct properties and array items. All ten routes accepted the referenced fixtures through their normal adapters; optional/open shapes still need fallback on some routes.

- Haiku through Vertex and the native Gateway changed requested data in one valid non-streaming repeated-reference sample each. Their streaming samples matched. Accepted schemas do not imply exact model output.
- OpenAI through Vercel Responses added `secondary: null` when asked to omit an optional nullable object, with both inline and referenced schemas and in both response modes. This satisfies the schema but changes the requested arguments. Real nulls are deliberately preserved because callers can distinguish null from absence.
- The optional non-nullable pair returned exact valid arguments on every route in both modes. On OpenAI Vercel Responses, the adapter restores omission markers through named references and simple nullable alternatives, preserving required values and genuine nulls.
- Four conflicting streaming pagination samples failed: two truncated calls on Haiku Messages and two malformed argument objects on Haiku native Gateway. These errors remain visible to callers.
- Numeric JSON schema values retain their numeric representation. Independent wire tests cover integers above JavaScript's exact-integer range.
- Large schemas and keyword combinations can still exceed provider limits. These fixtures do not prove acceptance of arbitrary application schemas. Gateway downstream conversion and billed schema tokens are not visible.

The previous provider probes motivated this support but did not establish adapter support. The current results supersede those probe-only claims. Definitions are local to one tool schema and are sent in the same request; they are not shared across tools or requests.

Raw runs and manifests are retained under `dist/schema-matrix/reference-support-{full,stream}-{other,gemini}`. Manifests record tested source hashes. The result summaries are generated by `scripts/schema_matrix_report.py`. Claude native coverage means Vertex, not Anthropic's hosted endpoint; Gemini native coverage means Vertex, not the Developer API.

## Compatibility reference and running the matrix


Every adapter checks function schemas before sending a request. Define inputs
with either `FunctionDeclaration.Parameters` (`genai.Schema`) or
`ParametersJsonSchema` (a JSON-serialisable schema object), never both. Tool
schemas must have an object root. The raw form uses JSON Schema 2020-12 (other explicitly declared drafts are
rejected rather than silently reinterpreted);
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
use `strict: false`. On the Vercel Responses route for OpenAI, the Gateway still
fills optional inputs. With explicit opt-in, the adapter allows null as an
omission marker for optional non-nullable typed fields, asks the model to use
it for absent inputs, and removes only these markers from final tool arguments.
Required fields and existing nullable fields keep their meaning. This conversion
walks direct properties, array items, named local references with annotation-only siblings, and simple value-or-null alternatives. It does not traverse arbitrary alternatives or references with assertion siblings. This does not
guarantee the model will omit every unrequested value; validate returned arguments.
Compatibility checking is distinct from provider strict decoding.

Compatibility profiles are conservative:

| Route | Behaviour |
|---|---|
| OpenAI Responses, direct or Vercel | Standard JSON Schema subset; unsupported composition rules require opt-in. Optional fields retain their original meaning. |
| Claude, direct or Vercel | Unsupported numeric bounds, length rules and unrestricted regex patterns require opt-in. Root alternatives require opt-in removal because Claude rejects that shape. Direct/Vertex nullable enums require explicit best-effort permission; Gateway nullable enums keep their checked strict-mode policy. |
| Gemini, direct or Vertex | `toolschema.WrapGemini` checks the input while the Google SDK retains ownership of its native typed format. |
| Gemini through Vercel | Checks known additional losses in the public Google converter, including numeric bounds, pattern and maximum string length. Root alternatives require opt-in removal. |

Presentation-only property ordering may be dropped with a warning. Unsupported
assertions are removed only with opt-in; unsupported `oneOf` can become `anyOf`
on OpenAI with an explicit exclusivity-loss warning. Claude and Google fallback
remove `oneOf` because the converted alternative shape can be rejected. Named local references (`#/$defs/name` or `#/definitions/name`) are checked and retained on every provider route. They must be acyclic. External references, anchors, root references, nested `$id` scopes and dynamic references remain errors, even with fallback enabled. Claude also rejects references inside `allOf`. This is not a complete
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

### Repeatable live schema matrix

The synthetic matrix compares the local catalogue with observed provider
behaviour. It never executes tools. Ordinary tests do not make these paid calls.

Provide `OPENAI_API_KEY`, `AI_GATEWAY_API_KEY`, `GOOGLE_CLOUD_PROJECT`, and
`GOOGLE_CLOUD_LOCATION` (or `GOOGLE_CLOUD_REGION`), plus Google application
default credentials with Vertex access. Missing credentials are setup failures.
The pinned models are Luna, Haiku 4.5 and Gemini 3.1 Flash Lite; route definitions
are in `schema_matrix_live_test.go`.

```sh
# Use a new, empty directory for every run.
ADK_SCHEMA_LIVE=1 ADK_SCHEMA_OUTPUT="$PWD/dist/schema-matrix/run-1" \
  go test . -run '^TestSchemaMatrixLive$' -parallel 8 -count=1 -timeout 25m
python3 scripts/schema_matrix_report.py dist/schema-matrix/run-1 \
  --output dist/schema-matrix/report-1
```

`ADK_SCHEMA_ROUTES`, `ADK_SCHEMA_CASES` and `ADK_SCHEMA_MODES` accept exact,
comma-separated selections. Unknown or duplicate names fail. Modes are
`default`, `fallback`, and `probe`; fallback runs only where default validation
rejects the case. `ADK_SCHEMA_STREAM=1` repeats a selection using streaming.
Leave it unset for ordinary responses. Repeat into separate output directories
to measure variation without mixing observations.

The default and fallback modes use the real adapter. Probe mode replaces a
neutral schema with the original case at the final HTTP boundary, bypassing
catalogue restrictions only in the test. For OpenAI and Claude probes this
retains the neutral schema's `strict: true`; it does not probe non-strict API
behaviour. Gemini has no equivalent strict flag. Captured schema fields show
what was sent to the direct API or Gateway, not the Gateway's private downstream
request. Native Vertex Gemini adapter cases include both raw and typed inputs;
provider probes use raw JSON Schema.

Each case has one valid and one deliberately conflicting prompt. Results record
HTTP status, final tool names and arguments, response errors and stop reasons,
original-schema conformance, prepared-schema conformance, and exact requested
argument matching. Format validation is explicitly enabled in the local oracle.
Claude forced-tool calls leave thinking unset; other routes use minimal thinking.

A passing Go test means **collection completed**, not that all schemas were
accepted or all arguments conformed. The report distinguishes local blocking,
HTTP errors, missing/wrong/multiple calls, truncation, nonconforming arguments,
and valid arguments that changed the requested values. It refuses incomplete or
failed collections unless `--allow-incomplete` is explicitly used for exploration.
Successful samples show observed conformance, not guaranteed enforcement.

Result directories contain synthetic schemas and arguments, not authentication
headers or reasoning text. Treat new cases as public synthetic fixtures. The
manifest records planned cases and source hashes; retain it with the raw results.

The [live schema matrix](README.md) records the latest
verification date, tested routes, observed support and remaining limits. Update
that single report when rerunning the matrix. Keep raw runs and their manifests
as evidence; do not add dated narrative reports. Backend argument validation
remains required.
