# Changelog

All notable changes to this project will be documented in this file.

## [v5.0.0] - Unreleased

### Changed

- Rebuilt the library around an explicit `Runtime` supervisor model.
- Replaced the v4 handle-based API with `Runner`, `Task`, `Service`, and `Job`.
- `Wait()` now returns runtime/task failures instead of acting as a pure blocker.
- Removed the core global default runtime API in favor of explicit ownership.
- Removed `Context.Exit()` and the `Handle` interface from the main API surface.
- Runtime shutdown is now explicit through `Shutdown(ctx)` and task completion is modeled by returning from the runner.
- `Service` now fails if it exits cleanly before shutdown, while `Job` remains finite work.

### Added

- `TaskError` for structured task failure reporting.
- `WithFailFast(false)` to keep the runtime alive after individual task failures.
- Dynamic `Add` support while the runtime is running.
- `WithJobPool(...)` to reuse worker goroutines for short-lived jobs.

### Removed

- `Handle`, `Context`, `DefaultAsync`, package-level `Register/Run/Start/Wait`, and signal-driven core lifecycle handling.

## [v4.2.0] - Unreleased

### Added

- Repository maintenance improvements:
  - Added `.goreleaser.yaml` for library release automation.
  - Added `Makefile` with common development targets (`test`, `lint`, `examples`, `release-snapshot`).
  - Added public `CONTRIBUTING.md` guide.
  - Added GitHub Issue and Pull Request templates.
  - Updated `README.md` with development, contribution, and release instructions.

### Changed

- Improved exported API documentation and example comments for easier onboarding.
- Refined internal shutdown orchestration in `async.go` without changing the v4 public API.

## [v4.1.0] - 2026-03-31

## [v4] - 2025-10-13

### Changed

- Breaking change: `NewAsync` no longer accepts a context or signal channel. Provide a context when calling `Async.Start(ctx)` or `Async.Run(ctx)` so you can register handles before starting execution.
- `Async.Run(ctx)` now blocks until all handles exit; `Async.Start(ctx)` remains non-blocking and returns a stop function for manual lifecycle control.
- Simplified shutdown handling: `NewAsync` and `Start` manage their own signal watcher via `signal.NotifyContext`.
- Replaced custom logger with `slog` structured logger; users can inject their own logger using `WithLogger(*slog.Logger)` option.
- Shutdown hooks are executed concurrently and awaited. Added `WithHookTimeout(time.Duration)` option to set a per-hook timeout.

### Added

- DefaultAsync: a global async instance for convenient cross-package task registration and coordination. Provides package-level Register/Wait functions for unified management, similar to prometheus.DefaultRegisterer.
- examples/default/main.go to demonstrate DefaultAsync usage.

### Removed

- `WithUseContextSignal` option removed; `NewAsync` always uses signal.NotifyContext to receive OS signals. Use `context` cancellation to trigger shutdown programmatically.

- Update your code to construct managers with `NewAsync(opts...)`, register handles, then either call `Run(ctx)` for a blocking lifecycle or `Start(ctx)` plus `Wait()` for asynchronous control. Use `WithHookTimeout` to bound hook execution times.
- `DefaultAsync` no longer starts automatically; call `async.Start(ctx)` or `async.Run(ctx)` in `main` after registering global handles.
- Module path updated to `github.com/dingdayu/async/v4`.
