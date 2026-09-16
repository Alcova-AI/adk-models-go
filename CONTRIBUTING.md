# Contributing

Bug reports and pull requests are welcome.

For substantial changes, open an issue first so we can agree on the
approach. Small fixes can go straight to a pull request.

## Bug reports

Include the Go and module versions, model, adapter and provider route,
whether streaming is enabled, and a minimal reproduction where possible.
Use synthetic inputs and remove credentials and private data from logs.

## Pull requests

- Explain the problem and how your change fixes it.
- Keep changes focused.
- Add or update tests when behaviour changes. For adapter changes, cover
  streaming and non-streaming behaviour where relevant.
- Use the Go version specified in [go.mod](go.mod) and format Go code with
  `gofmt`.
- Run `go test ./...` and `go vet ./...` before submitting. Run
  `go test -race -timeout 10m ./...` where supported; race detection requires
  a C compiler and CGO enabled. Say which checks you could not run.
- Do not include credentials, private data, or confidential material.

CI runs four checks: `test` (tests and vet), `lint`, `formatting`, and `race`.
To run the same lint checks locally, use golangci-lint v2.13.0:

```sh
golangci-lint run --no-config --enable-only=staticcheck,ineffassign,unused --timeout=5m
```

Live provider tests are opt-in, require credentials and may incur charges.
They are not required for every contribution. If a change affects provider
compatibility, describe the routes verified and any remaining gaps. See the
[live schema matrix](testdata/schema-matrix/README.md) for commands and
credential requirements. Never commit credentials or raw customer data.

## Licence and permission

By submitting a contribution for inclusion in this project, you agree
to license it under the [Apache License 2.0](LICENSE), without additional
terms.

You confirm that you have the right to submit the contribution under
that licence, including any required permission from your employer or
another copyright owner.

If your contribution includes third-party material, identify its source
and licence in the pull request. Preserve applicable copyright and
licence notices, and update [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
where needed.

You retain ownership of your contributions. No copyright assignment
or separate contributor licence agreement is required.
