# async

Safe asynchronous tasks manager for Go.

This library helps you supervise long-running background services and finite jobs in Go. v5 replaces the old handle-based API with an explicit runtime model built around named tasks and `context.Context`.

## Install

```bash
go get github.com/dingdayu/async/v5
```

## Quick start

v5 models work with one primitive:

- `async.Runner`: `func(context.Context) error`

You then wrap a runner as one of two task kinds:

- `async.Service(...)` for long-running workers that usually run until shutdown
- `async.Job(...)` for finite work that completes and returns

### Runtime

Create an explicit runtime, add tasks, then either block with `Run` or use `Start` + `Shutdown` + `Wait` for manual lifecycle control.

Jobs are executed through an internal worker pool by default so bursts of short-lived tasks can reuse goroutines more efficiently than spawning one goroutine per job. Use `async.WithJobPool(0)` if you want to disable pooling.

If you need backpressure control for short jobs, you can also bound the pooled queue with `async.WithJobQueue(n)`. In that mode:

- `Add(...)` blocks until queue capacity becomes available
- `TryAdd(...)` returns immediately with `async.ErrJobQueueFull` when the queue is saturated

**Typical usage:**

```go
import (
	"context"
	async "github.com/dingdayu/async/v5"
)

rt := async.NewRuntime()

_ = rt.Add(async.Service("worker", func(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// do work
		}
	}
}))

_ = rt.Add(async.Job("warm-cache", func(ctx context.Context) error {
	// finite startup work
	return nil
}))

if err := rt.Run(context.Background()); err != nil {
	panic(err)
}
```

### Bounded queued jobs

```go
rt := async.NewRuntime(
	async.WithJobPool(4),
	async.WithJobQueue(32),
)

err := rt.TryAdd(async.Job("send-email", func(ctx context.Context) error {
	// short-lived work
	return nil
}))
if errors.Is(err, async.ErrJobQueueFull) {
	// decide whether to retry, drop, or apply upstream backpressure
}
```

**When to use Services:**

- Consumers, pollers, stream processors, background sync loops
- Any worker that should stay alive until shutdown

**When to use Jobs:**

- Warm-up tasks, migrations, one-off background work, startup probes
- Finite work that should complete and return

### Service example

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	async "github.com/dingdayu/async/v5"
)

func main() {
	rt := async.NewRuntime()

	if err := rt.Add(async.Service("example", func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			default:
				fmt.Println("task running")
				time.Sleep(1 * time.Second)
			}
		}
	})); err != nil {
		panic(err)
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rt.Run(runCtx); err != nil {
		panic(err)
	}
}
```

### Manual lifecycle control

```go
rt := async.NewRuntime()
_ = rt.Add(async.Service("worker", runWorker))

parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := rt.Start(parentCtx); err != nil {
	panic(err)
}

<-parentCtx.Done()

shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := rt.Shutdown(shutdownCtx); err != nil {
	panic(err)
}

if err := rt.Wait(); err != nil {
	panic(err)
}
```

## Examples

See the `examples/` folder for runnable v5 examples:

- `examples/handle`: a long-running service example.
- `examples/task`: a runtime with explicit shutdown.
- `examples/default`: a runtime mixing jobs and services.

Run them with:

```bash
# run the handle example
go run ./examples/handle

# run the task example
go run ./examples/task

# run the mixed runtime example
go run ./examples/default
```

## v4 to v5 migration notes

- `Handle` / `Context.Exit()` are removed from the core API.
- Package-level global registration is removed from the core API.
- Cleanup should use normal `defer` inside the runner.
- `Wait()` now returns the first task error, if any.
- Long-running workers should usually be modeled as `Service`, while finite work should be modeled as `Job`.

The first breaking release focuses on clearer lifecycle semantics, error propagation, and explicit runtime ownership.

It also treats `Service` and `Job` differently on purpose:

- `Service` is expected to keep running until shutdown. A clean early return is treated as a runtime failure.
- `Job` is expected to finish by returning.

## Development

We use a `Makefile` to manage common development tasks.

- **Run tests:** `make test`
- **Run linting:** `make lint`
- **Run examples:** `make examples`

The equivalent raw commands are:

```bash
go test ./...
golangci-lint run
go run ./examples/default
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details on our development workflow and how to submit a pull request.

## Release

This project uses [GoReleaser](https://goreleaser.com/) for releases. You can test the release process locally using:

```bash
make release-snapshot
```

The active breaking-development line is `v5`.
