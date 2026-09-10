# Live tool schema matrix

Last tested: **10 September 2026** (Australia/Melbourne).

## Scope

33 schema cases in non-streaming mode, with six selected cases repeated in streaming mode. Each case requests one valid and one conflicting argument object. Default mode rejects unsupported contracts locally; fallback mode runs only when default mode rejects the schema and explicitly allows weaker enforcement. No tools or customer actions execute.

| Route label | Model | Connection |
|---|---|---|
| luna-direct | `gpt-5.6-luna` | OpenAI Responses |
| luna-native / luna-responses | `gpt-5.6-luna` | Vercel native / Responses |
| haiku-vertex | `claude-haiku-4-5` | Vertex |
| haiku-native / haiku-messages | `anthropic/claude-haiku-4.5` | Vercel native / Messages |
| gemini-vertex | `gemini-3.1-flash-lite` | Vertex |
| gemini-native / gemini-responses / gemini-messages | `google/gemini-3.1-flash-lite` | Vercel native / Responses / Messages |

Completed coverage: **10 routes**, 1,266 records and 766 HTTP attempts. **383/383 valid attempted requests** returned the requested valid arguments. Local rejections make no HTTP request. These totals are test observations, not production success rates.

## Supported behaviour and limits

- Numeric schema bounds, cardinalities and enum values retain their numeric JSON representation. Wire tests also preserve integers above JavaScript’s exact-integer range.
- Typed and raw nullable enums accept null in the tested routes. Claude uses an equivalent type-union form; direct/Vertex nullable enums require explicit best-effort permission because strict mode rejects that form.
- OpenAI through Vercel Responses supports omission of optional non-nullable typed fields through an explicit fallback: the adapter sends null markers and removes only those markers from final arguments. Required values, real nulls and zero are preserved. Conversion covers direct properties and array items, not alternatives or references.
- The original client-list schema passed three additional authenticated backend samples through Vercel Responses, including the real argument decoder, protobuf validation and pagination parser.
- Unsupported constraints fail locally by default. With explicit fallback, they may be removed or weakened with warnings. Claude and Gemini Gateway root alternatives require this fallback; Claude and Google oneOf are removed instead of converted to rejected shapes.
- Static local references remain supported only on OpenAI routes in this adapter. Other reference conversions remain local errors, including with fallback enabled.
- Schema acceptance and a strict flag do not guarantee argument enforcement. Backend validation remains required. The table below distinguishes observed conformance from weakened or violated contracts.

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

See the [README live-matrix instructions](../../README.md#repeatable-live-schema-matrix). The maintained fixtures and independent positive/negative oracles live in `schema_matrix_cases_test.go`. Wire tests check serialisation separately from provider behaviour.

Each raw run retains its collection manifest, planned case identities, source hashes, outgoing schema, strict flags, warnings, HTTP status, final tool name, stop reason, errors and argument-validation results. Source manifests identify the implementation tested by each run. For repeated route/case pairs, the latest complete run replaces the earlier results. Raw provider probes are available as a separate mode; the results above use the checked adapter paths, not probes.

Coverage does not establish all schema dialects, keyword combinations, formats, model versions or complexity limits. Claude direct coverage means Vertex, not the Anthropic-hosted endpoint; Gemini direct coverage means Vertex, not the Developer API. Gateway downstream requests are not visible.
