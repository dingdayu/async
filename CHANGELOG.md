# Changelog

All notable changes to this project will be documented in this file.

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
