# Contributing to async

Thank you for your interest in contributing to `async`! This project is a safe asynchronous tasks manager for Go.

## Code of Conduct

Please follow standard open-source citizenship practices. Be respectful and collaborative.

## Getting Started

1. Fork the repository.
2. Clone your fork locally.
3. Install dependencies: `go mod download`.
4. Ensure you have Go and `golangci-lint` installed.

## Development Workflow

We use a `Makefile` to manage common development tasks.

- Run tests: `make test`
- Run linting: `make lint`
- Run example programs: `make examples`
- Run a local release snapshot: `make release-snapshot`

### Submitting a Pull Request

1. Create a new branch for your changes: `git checkout -b feat/your-feature-name`.
2. Write tests for any new functionality.
3. Ensure all tests pass and linting is clean.
4. Commit your changes with clear messages.
5. Push to your fork and open a Pull Request against the `v4` branch.

## Release Process

This project uses [GoReleaser](https://goreleaser.com/) for releases. Releases follow [Semantic Versioning](https://semver.org/).

To test the release process locally:
```bash
make release-snapshot
```

The current stable line is `v4`. Please ensure your PRs target the correct branch.

## Documentation Expectations

Please update README examples, Godoc comments, and changelog entries when a change affects user-visible behavior or contributor workflow.
