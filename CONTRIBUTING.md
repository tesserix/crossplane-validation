# Contributing to crossplane-validate

Thanks for your interest in contributing! This guide will help you get started.

## Getting Started

1. Fork the repo
2. Clone your fork: `git clone https://github.com/<your-user>/crossplane-validation.git`
3. Create a branch: `git checkout -b feat/your-feature`
4. Make your changes
5. Run tests: `go test ./...`
6. Push and open a PR against `main`

## Prerequisites

- Go 1.26+
- Git

## Development

```bash
# Build
go build -o crossplane-validate ./cmd/crossplane-validate

# Run tests
go test ./... -v

# Run the CLI
./crossplane-validate plan --manifests=testdata/sample-manifests/
```

## What to Contribute

- New cloud provider resource mappings (see `pkg/hcl/converter.go`)
- Bug fixes
- Documentation improvements
- Test coverage for additional Crossplane resource types
- Performance improvements

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions small and focused
- Write tests for new functionality
- No unnecessary comments — code should be self-explanatory

## Pull Request Process

1. Ensure all tests pass (`go test ./...`)
2. Update testdata if adding new resource type support
3. Keep PRs focused — one feature or fix per PR
4. PRs require approval from at least one maintainer before merge

## Reporting Issues

Use [GitHub Issues](https://github.com/tesserix/crossplane-validation/issues) to report bugs or request features. Include:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Crossplane version and provider versions

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
