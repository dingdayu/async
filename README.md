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
- `async.JobWithPriority(...)` (or `async.Job(...).WithPriority(...)`) for finite work that should be queued ahead of lower-priority jobs
- `async.JobInPartition(...)` (or `async.Job(...).WithPartition(...)`) for routing finite work to specific execution partitions
- `task.WithMiddleware(...)` or `WithTaskMiddleware(...)` for task wrappers such as logging, tracing, metrics, timeout, or retry

## API layers

The v5 runtime is intentionally organized into three layers:

- **Core runtime layer**: `Runtime`, `Task`, `Runner`, `Job`, `Service`, `Add`, `TryAdd`, `Start`, `Run`, `Shutdown`, `Wait`
- **Control layer**: pooling, queue capacity, queue full policy, partitions, priority
- **Extension layer**: middleware/wrappers, observers, stats snapshots

That split is intentional: the core stays small, control stays explicit, and cross-cutting behavior lives outside the scheduler hot path.

### Runtime

Create an explicit runtime, add tasks, then either block with `Run` or use `Start` + `Shutdown` + `Wait` for manual lifecycle control.

**Tasks:**

- `async.Service("name", runner)`: Long-running workers. If it returns `nil` before the context is canceled, it is considered an error (`ErrUnexpectedServiceExit`).
- `async.Job("name", runner)`: Finite work. It is expected to return `nil` upon completion.

**Job Pool and Backpressure:**

Jobs are executed through an internal worker pool by default so bursts of short-lived tasks can reuse goroutines more efficiently than spawning one goroutine per job. 

- `WithJobPool(n)`: Sets the number of worker goroutines (default is `GOMAXPROCS`). Use 0 to disable pooling.
- `WithJobQueue(n)`: Sets the queue capacity. Default is unbounded (0).
- `Add(task)`: Blocks until queue capacity becomes available if it's a pooled Job.
- `TryAdd(task)`: Returns `async.ErrJobQueueFull` immediately if the queue is saturated.
- `WithQueueFullPolicy(policy)`: Controls full-queue behavior for bounded pooled jobs.
  - `QueueFullBlock` (default): `Add` blocks, `TryAdd` returns `ErrJobQueueFull`
  - `QueueFullReject`: both `Add` and `TryAdd` return `ErrJobQueueFull`
  - `QueueFullDropOldest`: drop the oldest queued job, then enqueue the new job
  - `QueueFullDropNewest`: drop the newest queued job, then enqueue the new job
- `WithJobPartition(name, config)`: Configures a named execution partition for jobs. Each partition has its own worker pool and queue settings.
- `WithTaskMiddleware(middleware...)`: Registers runtime-wide task wrappers.
- `WithObserver(observer...)`: Subscribes to runtime lifecycle events such as task queueing, start, finish, and queue drops.
- Pooled job queues are priority-aware: higher `Task.Priority` values run first, while jobs with the same priority keep FIFO ordering.
- Internally, pooled job partitions now use a heap-backed priority queue and per-partition synchronization to reduce contention while preserving the same public semantics.

**Observability:**

- `Runtime.Stats()` returns a point-in-time snapshot of runtime and partition state.
- `RuntimeStats.Running` counts currently executing tasks; queued pooled jobs remain visible through partition `QueueLen`.
- Observers receive `runtime_started`, `runtime_finished`, `task_added`, `task_queued`, `task_started`, `task_finished`, and `task_dropped` events.
- `task_dropped` includes the `QueueFullPolicy` that caused the drop, which is useful when using `QueueFullDropOldest` or `QueueFullDropNewest`.
- Observer panics are isolated from runtime control flow, but observers should still stay fast and non-blocking.

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
	async.WithQueueFullPolicy(async.QueueFullReject),
)

err := rt.TryAdd(async.Job("send-email", func(ctx context.Context) error {
	// short-lived work
	return nil
}))
if errors.Is(err, async.ErrJobQueueFull) {
	// decide whether to retry, drop, or apply upstream backpressure
}
```

### Runtime observers

```go
rt := async.NewRuntime(
	async.WithObserver(async.ObserverFunc(func(event async.Event) {
		log.Printf("event=%s task=%s err=%v", event.Type, event.Task.Name, event.Err)
	})),
)

_ = rt.Add(async.Job("warm-cache", func(ctx context.Context) error {
	return nil
}))

if err := rt.Run(context.Background()); err != nil {
	panic(err)
}

stats := rt.Stats()
log.Printf("running=%d queued=%d", stats.Running, stats.Partitions[async.DefaultJobPartition].QueueLen)
```

### Task middleware

```go
logging := func(task async.Task, next async.Runner) async.Runner {
	return func(ctx context.Context) error {
		log.Printf("starting %s", task.Name)
		err := next(ctx)
		log.Printf("finished %s err=%v", task.Name, err)
		return err
	}
}

rt := async.NewRuntime(async.WithTaskMiddleware(logging))

_ = rt.Add(async.Job("warm-cache", func(ctx context.Context) error {
	return nil
}))

_ = rt.Add(async.Job("one-off", runOneOff).WithMiddleware(logging))
```

### Job Partitions

Partitions allow you to isolate different types of background work. For example, you can have a "critical" partition with many workers and a "batch" partition with fewer workers.

```go
rt := async.NewRuntime(
	async.WithJobPartition("batch", async.JobPartitionConfig{
		Workers:  2,
		QueueCap: 100,
	}),
)

// Route a job to the partition
_ = rt.Add(async.JobInPartition("process-video", "batch", func(ctx context.Context) error {
	// ...
	return nil
}))

// Or using WithPartition
_ = rt.Add(async.Job("generate-report", func(ctx context.Context) error {
	// ...
	return nil
}).WithPartition("batch"))
```

By default, jobs run in the `default` partition. Global settings like `WithJobPool` and `WithJobQueue` apply to the `default` partition.

**Note:** The current implementation of partitions does not support worker stealing or global fairness across partitions. Each partition's queue and workers are independent.

## Explicit non-goals

These are intentionally not part of the current design:

- restarting or rebooting a finished runtime
- removing the `Service` / `Job` distinction
- global fairness scheduling across partitions
- worker stealing across partitions
- dynamic partition rebalancing
- hiding task failures behind fire-and-forget APIs

The runtime is meant to stay explicit about lifecycle, failure propagation, and partition isolation.

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

For a fuller migration guide, see [MIGRATION_v5.md](MIGRATION_v5.md).

For the current development order after the v5 runtime refactor, see [ROADMAP.md](ROADMAP.md).

## Development

We use a `Makefile` to manage common development tasks.

- **Run tests:** `make test`
- **Run linting:** `make lint`
- **Run benchmarks:** `make bench`
- **Run examples:** `make examples`

The equivalent raw commands are:

```bash
go test ./...
golangci-lint run
go test ./... -run '^$' -bench 'BenchmarkRuntime' -benchmem
go run ./examples/default
```

The benchmark suite currently focuses on runtime/job hot paths:

- plain Job execution versus pooled Job execution
- heap-backed priority queue submission
- partitioned submission overhead
- partition isolation under pressure

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details on our development workflow and how to submit a pull request.

## Release

This project uses [GoReleaser](https://goreleaser.com/) for releases. You can test the release process locally using:

```bash
make release-snapshot
```

The active breaking-development line is `v5`.
