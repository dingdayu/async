# AGENTS Guide

## Dev environment tips

- Use `go run ./examples/<name>` to explore the sample apps (`handle`, `task`, `default`) described in `README.md` instead of manually wiring small repros.
- Run `go env GOPATH` if you need to confirm your module cache; the project module path is `github.com/dingdayu/async/v5`, so imports should follow that prefix.
- Prefer `async.NewRuntime()` followed by `Run(ctx)` while experimenting—this blocks until tasks exit and mirrors the quick-start example in the README.
- When you need non-blocking behaviour, call `Start(ctx)`, then later `Shutdown(ctx)` and `Wait()` explicitly.

## Testing instructions

- Execute `go test ./...` before opening a pull request; this matches the guidance in the README quick-start section and ensures all packages compile.
- Run `golangci-lint run` to mirror the CI workflow (`.github/workflows/golangci-lint.yml`). Fix lint issues before committing.
- Run `make bench` when changing queueing, pooling, or partition behavior so you can compare runtime hot paths before and after the change.
- For example programs, use `go run ./examples/<name>` to confirm they still produce the documented output.
- If you add new runtime or task lifecycle behaviour, create focused tests under `async_test.go` using the synchronous `Run` API so the tests block until shutdown.
- Update or add tests whenever behaviour changes—especially around shutdown, error propagation, and dynamic task management.

## PR instructions

- Title format: `[async] <Short summary>` (include the module name for easier filtering).
- Always run `golangci-lint run` and `go test ./...` prior to pushing; both must pass.
- Call out in the PR description whether your change affects the public API or the lifecycle semantics (`Run` vs `Start`), since downstream users rely on those contracts.
