# Changelog

All notable changes to this project will be documented in this file.

## [v4] - 2025-10-13

### Changed

- Breaking change: removed the `ch <- chan os.Signal` parameter from `NewAsync`. The function now uses `signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)` internally to handle OS signals.
- Simplified shutdown handling: `NewAsync` no longer requires an external signal channel.
- Replaced custom logger with `slog` structured logger; users can inject their own logger using `WithLogger(*slog.Logger)` option.
- Shutdown hooks are executed concurrently and awaited. Added `WithHookTimeout(time.Duration)` option to set a per-hook timeout.

### Added

- DefaultAsync: a global async instance for convenient cross-package task registration and coordination. Provides package-level Register/Wait functions for unified management, similar to prometheus.DefaultRegisterer.
- examples/default/main.go to demonstrate DefaultAsync usage.

### Removed

- `WithUseContextSignal` option removed; `NewAsync` always uses signal.NotifyContext to receive OS signals. Use `context` cancellation to trigger shutdown programmatically.

### Notes

- Update your code to call `NewAsync(ctx)` without a signal channel and, if necessary, use `WithHookTimeout` to bound hook execution times.
- Module path updated to `github.com/dingdayu/async/v4`.
