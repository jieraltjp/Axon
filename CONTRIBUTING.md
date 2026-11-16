# Contributing to Axon

Thanks for your interest! Axon is an early prototype, so feedback on the API and on real use cases is as valuable as code.

## Ways to help

- **Try it on a real CLI** and [open an issue](https://github.com/jieraltjp/Axon/issues) about what worked and what didn't.
- **Report bugs** with the command you ran, the output you got, and what you expected.
- **Propose API changes** in an issue before starting large pull requests.

## Development

Requires Go 1.25 or newer.

```bash
go test ./...                       # unit tests
go vet ./...
gofmt -l .                          # must print nothing
go run ./examples/notes --help      # try the example
```

## Guidelines

- **Machine contracts are an API.** Output shapes, error kinds and codes, exit codes and the `schema` manifest must not change without a clear reason and a note in the pull request.
- **Keep the core small.** New dependencies need a strong justification; the core currently depends only on Cobra and go-isatty.
- **Every behavior needs a test.** Tests in `axon_test.go` show the pattern: build an app with `newHarness`, run it, assert on stdout, stderr and the exit code.
- **Benchmarks report what they measure.** If you change `bench/`, keep the results honest and reproducible, including results that don't favor Axon.

## License

By contributing, you agree that your contributions are licensed under the [Apache-2.0 License](LICENSE).
