# AGENTS Guide

## Dev environment tips

- Use `go run ./examples/<name>` to explore the sample apps (`handle`, `task`, `default`) described in `README.md` instead of manually wiring small repros.
- Run `go env GOPATH` if you need to confirm your module cache; the project module path is `github.com/dingdayu/async/v4`, so imports should follow that prefix.
- Prefer `async.NewAsync()` followed by `Run(ctx)` while experimenting—this blocks until tasks exit and mirrors the quick-start example in the README.
- When you need non-blocking behaviour, call `Start(ctx)` and keep the returned stop function handy; defer it so your local process shuts down cleanly.

## Testing instructions

- Execute `go test ./...` before opening a pull request; this matches the guidance in the README quick-start section and ensures all packages compile.
- Run `golangci-lint run` to mirror the CI workflow (`.github/workflows/golangci-lint.yml`). Fix lint issues before committing.
- For example programs, use `go run ./examples/<name>` to confirm they still produce the documented output.
- If you add new handles or lifecycle changes, create focused tests under `async_test.go` using the synchronous `Run` API so the tests block until shutdown.
- Update or add tests whenever behaviour changes—especially around shutdown hooks or context handling.

## PR instructions

- Title format: `[async] <Short summary>` (include the module name for easier filtering).
- Always run `golangci-lint run` and `go test ./...` prior to pushing; both must pass.
- Call out in the PR description whether your change affects the public API or the lifecycle semantics (`Run` vs `Start`), since downstream users rely on those contracts.
