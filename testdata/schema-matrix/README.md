# Live tool schema matrix

Last tested: **10 September 2026** (Australia/Melbourne).

## Scope

39 schema cases, including six new paired inline/reference cases tested in both non-streaming and streaming modes. Each case requests one valid and one conflicting argument object. Default mode rejects unsupported contracts locally; fallback mode runs only when default mode rejects the schema and explicitly allows weaker enforcement. No tools or customer actions execute.

| Route label | Model | Connection |
|---|---|---|
| luna-direct | `gpt-5.6-luna` | OpenAI Responses |
| luna-native / luna-responses | `gpt-5.6-luna` | Vercel native / Responses |
| haiku-vertex | `claude-haiku-4-5` | Vertex |
| haiku-native / haiku-messages | `anthropic/claude-haiku-4.5` | Vercel native / Messages |
| gemini-vertex | `gemini-3.1-flash-lite` | Vertex |
| gemini-native / gemini-responses / gemini-messages | `google/gemini-3.1-flash-lite` | Vercel native / Responses / Messages |

The original 33-case baseline covered **10 routes**, 1,266 records and 766 HTTP attempts. **383/383 valid attempted requests** returned the requested valid arguments. Local rejections make no HTTP request. These baseline totals exclude the new reference comparison below and are test observations, not production success rates.

## Supported behaviour and limits

- Numeric schema bounds, cardinalities and enum values retain their numeric JSON representation. Wire tests also preserve integers above JavaScript’s exact-integer range.
- Typed and raw nullable enums accept null in the tested routes. Claude uses an equivalent type-union form; direct/Vertex nullable enums require explicit best-effort permission because strict mode rejects that form.
- OpenAI through Vercel Responses supports omission of optional non-nullable typed fields through an explicit fallback: the adapter sends null markers and removes only those markers from final arguments. Required values, real nulls and zero are preserved. Conversion covers direct properties and array items, not alternatives or references.
- The original client-list schema passed three additional authenticated backend samples through Vercel Responses, including the real argument decoder, protobuf validation and pagination parser.
- Unsupported constraints fail locally by default. With explicit fallback, they may be removed or weakened with warnings. Claude and Gemini Gateway root alternatives require this fallback; Claude and Google oneOf are removed instead of converted to rejected shapes.
- Static local references remain supported only on OpenAI routes in this adapter. Other reference conversions remain local errors, including with fallback enabled.
- Schema acceptance and a strict flag do not guarantee argument enforcement. Backend validation remains required. The tables below distinguish adapter support from provider-only probes and observed conformance.

## Non-streaming results

Each cell describes the valid/conflicting pair using default mode where available, otherwise explicit fallback:

- **S**: both returned argument objects satisfied the original schema; the valid request also matched exactly.
- **W**: at least one returned argument object violated the original schema.
- **E**: at least one call failed, was interrupted, or produced malformed arguments.
- **L**: the adapter rejected the schema even with fallback.
- **\***: explicit fallback was required. Even **S\*** does not mean the original constraints were preserved or enforced by the provider.

A single pair is a small sample. **S** means observed conformance for this model and route, not guaranteed support for every combination of the rule.

| Rule | luna-direct | luna-native | luna-responses | haiku-vertex | haiku-native | haiku-messages | gemini-vertex | gemini-native | gemini-responses | gemini-messages |
|---|---|---|---|---|---|---|---|---|---|---|
| allOf | W* | W* | W* | W* | W* | W* | W* | W* | W* | W* |
| anyOf | S | S | S | S | W | W | S | S* | S* | S* |
| closed-object | S | S | S | S | S | S | S | S* | S* | S* |
| const | W* | W* | W* | S | S | W | W* | S* | S* | S* |
| enum | S | S | S | S | S | W | S | S* | S* | S* |
| exclusiveMaximum | S | S | S | W* | W* | W* | W* | W* | W* | W* |
| exclusiveMinimum | S | S | S | W* | W* | W* | W* | W* | W* | W* |
| format-date | S | S | S | S | W | W | S | S* | S* | S* |
| integer | S | S | S | S | S | W | S | S* | S* | S* |
| legacy-list-clients-typed | W* | W* | S* | W* | W* | W* | S | S | S | S |
| local-ref | S | S | S | L | L | L | L | L | L | L |
| maxItems | S | S | S | W* | W* | W* | S | S* | W* | W* |
| maxLength | S | S | S | W* | W* | W* | S | W* | W* | W* |
| maxProperties | W* | W* | W* | W* | W* | W* | W | W* | W* | W* |
| maximum | S | S | S | W* | W* | W* | S | W* | W* | W* |
| minItems-one | S | S | S | S | S | W | S | S* | W* | W* |
| minItems-two | S | S | S | W* | W* | W* | S | S* | W* | W* |
| minItems-two-typed | W* | W* | S* | W* | W* | W* | S | S | W | W |
| minLength | S | S | S | W* | W* | W* | W | W* | W* | W* |
| minProperties | W* | W* | W* | W* | W* | W* | W | W* | W* | W* |
| minimum | S | S | S | W* | W* | W* | S | W* | W* | W* |
| multipleOf | S | S | E | W* | W* | W* | W* | W* | W* | W* |
| nullable-enum | S | S | S | W* | W | W | S | S* | S* | S* |
| nullable-enum-typed | W* | W* | S* | W* | W* | W* | S | S | S | S |
| oneOf | W* | W* | W* | W* | W* | W* | W* | W* | W* | W* |
| optional-pagination | W* | W* | S* | W* | W* | W* | S | S | S | S |
| optional-pagination-typed | W* | W* | S* | W* | W* | W* | S | S | S | S |
| pagination-not | W* | W* | W* | W* | W* | W* | W* | W* | W* | W* |
| pattern | S | S | S | W* | W* | W* | S | W* | W* | W* |
| required | S | S | S | S | S | W | S | S* | S* | S* |
| root-anyOf | W* | W* | W* | W* | W* | W* | W* | W* | W* | W* |
| root-oneOf | W* | W* | W* | W* | W* | W* | W* | W* | W* | W* |
| uniqueItems | W* | W* | W* | W* | W* | W* | W* | W* | W* | W* |
| nullable-object-inline | S | S | S | S | S | W | S | S* | S* | S* |
| nullable-object-ref | S | S | S | L | L | L | L | L | L | L |
| optional-object-inline | W* | W* | W* | S | S | W | S | S* | S* | S* |
| optional-object-ref | W* | W* | W* | L | L | L | L | L | L | L |
| repeated-object-inline | S | S | S | S | S | W | S | S* | S* | S* |
| repeated-object-ref | S | S | S | L | L | L | L | L | L | L |

## Streaming coverage

Tested `minimum`, `minItems-one`, typed nullable enum, raw and typed optional pagination, and the original typed client-list schema on the completed routes. Only final tool calls are judged or passed to backend validation; partial deltas remain provider-native.

| Route | Valid requests matching exactly | Call errors |
|---|---:|---:|
| luna-direct | 6/6 | 0 |
| luna-native | 6/6 | 0 |
| luna-responses | 6/6 | 0 |
| haiku-vertex | 6/6 | 0 |
| haiku-native | 6/6 | 2 |
| haiku-messages | 6/6 | 2 |
| gemini-vertex | 6/6 | 0 |
| gemini-native | 6/6 | 0 |
| gemini-responses | 6/6 | 0 |
| gemini-messages | 6/6 | 0 |

## Failure observations

- `haiku-messages`, `optional-pagination`: 1 malformed or interrupted call(s), all from conflicting prompts.
- `haiku-messages`, `optional-pagination-typed`: 1 malformed or interrupted call(s), all from conflicting prompts.
- `haiku-native`, `optional-pagination`: 1 malformed or interrupted call(s), all from conflicting prompts.
- `haiku-native`, `optional-pagination-typed`: 1 malformed or interrupted call(s), all from conflicting prompts.
- `luna-responses`, `multipleOf`: 1 malformed or interrupted call(s), all from conflicting prompts.

Conflicting prompts also produced schema violations, particularly under explicit fallback and on Claude Gateway routes. Errors remain visible to callers and are not treated as successful tool execution.

## Reproduction and evidence

See the [live-matrix instructions](#repeatable-live-schema-matrix). The maintained fixtures and independent positive/negative oracles live in `schema_matrix_cases_test.go`. Wire tests check serialisation separately from provider behaviour.

Each raw run retains its collection manifest, planned case identities, source hashes, outgoing schema, strict flags, warnings, HTTP status, final tool name, stop reason, errors and argument-validation results. Source manifests identify the implementation tested by each run. For repeated route/case pairs, the latest complete run replaces the earlier results. Raw provider probes are available as a separate mode; the results above use the checked adapter paths, not probes.

Coverage does not establish all schema dialects, keyword combinations, formats, model versions or complexity limits. Claude direct coverage means Vertex, not the Anthropic-hosted endpoint; Gemini direct coverage means Vertex, not the Developer API. Gateway downstream requests are not visible.

## Repeated-object reference comparison

Last tested: **10 September 2026**. Each pair uses the same argument contract and prompts: one schema repeats the objects inline, the other uses `$defs` and local `$ref`. References span two definition levels, direct properties and array items. Separate pairs cover explicit null and omission of an optional nullable object. These are synthetic tool definitions, not production RPC schemas.

This comparison contains 678 records and 422 HTTP attempts across all ten routes. Of 211 valid attempted requests, 195 returned the exact requested arguments. Counts include adapter calls and separate provider probes; local blocks make no HTTP request.

### Adapter results

The main table above includes the new non-streaming cases. Streaming produced the same S/W/L classifications for all six new cases. The adapter still blocks references on Claude and Gemini, including fallback. OpenAI routes accept the repeated and nullable reference schemas; optional schemas require fallback. No production reference support was added by this comparison.

### Provider probes

Probes bypass the local adapter check and replace the schema at the final HTTP boundary. **S** means both samples conformed and the valid sample matched exactly; **W** means a violation or changed valid arguments; **E** means an API or call error. Each cell is **non-streaming / streaming**. These observations do not establish end-to-end adapter support.

| Route | Repeated objects with refs | Nullable object with refs | Optional object with refs |
|---|---|---|---|
| luna-direct | S / S | S / S | E / E |
| luna-native | S / S | S / S | E / E |
| luna-responses | S / S | S / S | W / W |
| haiku-vertex | S / S | S / S | S / S |
| haiku-native | S / S | S / S | S / S |
| haiku-messages | W / W | W / W | W / W |
| gemini-vertex | S / S | S / S | S / S |
| gemini-native | S / S | S / S | S / S |
| gemini-responses | S / S | S / S | S / S |
| gemini-messages | S / S | S / S | S / S |

- All ten routes accepted the repeated and explicit-null reference probes, and all their valid samples matched exactly. Claude through the Messages Gateway returned the invalid requested country in conflicting prompts, for both inline and reference forms.
- Optional-object probes on direct OpenAI and the native OpenAI Gateway returned HTTP 400 because probe mode retains `strict: true` while the schema has an optional property. The inline controls failed identically. These are strict-mode shape rejections, not evidence that references are unsupported.
- OpenAI through Vercel Responses added `"secondary": null` when the valid prompt omitted that property, with both inline and reference forms, in both adapter fallback and probes. No secondary object data was invented in these samples. The returned object satisfied the schema but did not match the requested arguments. Whether this changes execution depends on the caller: some tools treat null as omission, while others distinguish null from an absent property. The existing omission conversion does not cover references or nullable alternatives.
- Claude and Gemini reference probes are promising, but their normal adapter paths remain blocked. Gateway wire captures confirm refs reached the Gateway; its private downstream conversion and billed schema token count are not visible.

### Schema size

Compact JSON token estimates using `o200k_base`, before provider conversion:

| Paired fixture | Inline | References |
|---|---:|---:|
| repeated-object | 265 | 158 |
| nullable-object | 187 | 147 |
| optional-object | 185 | 145 |

These are input-schema estimates, not provider-reported billing tokens. The definitions are sent once per tool schema; they are not shared across tools or requests.

### Reproduce this comparison

Run the following after configuring the API keys and Google credentials described below. Use a fresh output directory each time. Vertex Gemini uses `global`; Vertex Claude uses `us-east5` for this comparison. The first command covers the other nine routes.

```sh
export ADK_SCHEMA_CASES=local-ref,repeated-object-inline,repeated-object-ref,nullable-object-inline,nullable-object-ref,optional-object-inline,optional-object-ref
export ADK_SCHEMA_MODES=default,fallback,probe
ADK_SCHEMA_LIVE=1 GOOGLE_CLOUD_LOCATION=us-east5 \
  ADK_SCHEMA_ROUTES=luna-direct,luna-native,luna-responses,haiku-vertex,haiku-native,haiku-messages,gemini-native,gemini-responses,gemini-messages \
  ADK_SCHEMA_OUTPUT="$PWD/dist/schema-matrix/refs-other" \
  go test . -run '^TestSchemaMatrixLive$' -parallel 8 -count=1 -timeout 12m
ADK_SCHEMA_LIVE=1 GOOGLE_CLOUD_LOCATION=global ADK_SCHEMA_ROUTES=gemini-vertex \
  ADK_SCHEMA_OUTPUT="$PWD/dist/schema-matrix/refs-gemini" \
  go test . -run '^TestSchemaMatrixLive$' -parallel 8 -count=1 -timeout 12m
```

Repeat with `ADK_SCHEMA_STREAM=1`, new output directories and the six object cases (omit `local-ref`) for the streaming comparison. The initial Gemini attempt in `us-east5` returned model-route 404 errors; the complete `global` rerun supersedes those observations. Raw manifests retain both runs.

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
walks direct properties and array items, not alternatives or references. This does not
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
remove `oneOf` because the converted alternative shape can be rejected. Local static references are checked,
but reference conversion outside OpenAI and dynamic references remain errors
rather than being silently erased. This is not a complete
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
