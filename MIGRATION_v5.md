# Migrating from v4 to v5

v5 is a breaking redesign of `async`.

The project no longer centers on `Handle`, `Context.Exit()`, and the package-level default runtime. Instead, v5 uses an explicit `Runtime` plus `Service` and `Job` tasks built on `func(context.Context) error`.

## Release notes summary

### What changed

- Module path changed to `github.com/dingdayu/async/v5`
- The main abstraction is now `Runtime`
- `Handle` and `Context` are removed from the core API
- Package-level global runtime helpers are removed from the core API
- `Wait()` now returns task/runtime errors
- `Service` exits are treated differently from `Job` exits
- Short-lived jobs can run through an internal worker pool
- Bounded queue backpressure is available through `WithJobQueue(...)` and `TryAdd(...)`
- Pooled jobs can now carry priority and queue-full policies

### What stayed conceptually similar

- You still register named units of background work
- You still run them under a parent `context.Context`
- You still get graceful shutdown by canceling the parent context or calling `Shutdown(ctx)`

## API mapping

| v4 | v5 |
|---|---|
| `NewAsync()` | `NewRuntime()` |
| `Handle` | `Service(...)` or `Job(...)` |
| `Handle(ctx async.Context)` | `func(context.Context) error` |
| `ctx.Exit()` | `return nil` |
| `async.Register(...)` | `rt.Add(...)` |
| `async.Run(ctx)` | `rt.Run(ctx)` |
| `async.Start(ctx)` | `rt.Start(ctx)` |
| `async.Wait()` | `rt.Wait()` |
| package-level default runtime | explicit runtime ownership |

## Concept changes

### 1. Explicit runtime ownership

v4 encouraged a package-level default runtime. v5 expects you to create and pass around your own runtime:

```go
rt := async.NewRuntime()
```

This removes hidden global coupling and makes lifecycle ownership clearer.

### 2. `Handle` becomes `Service` or `Job`

In v4, every task looked similar even if some were long-running loops and others were finite work.

In v5:

- `Service` is for long-running workers
- `Job` is for finite work

This lets the runtime treat unexpected service exits as failures while allowing jobs to complete normally.

### 3. `ctx.Exit()` is gone

In v4, tasks had to signal completion through the custom context.

In v5, completion is just returning from the runner:

```go
return nil
```

### 4. Shutdown is explicit

v5 does not hide signal handling inside the core runtime. The application owns signal wiring:

```go
signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := rt.Run(signalCtx); err != nil {
	panic(err)
}
```

### 5. `Wait()` returns errors

`Wait()` now returns the first task failure, making async lifecycle control symmetric with `Run()`.

### 6. Job Pools and Backpressure

v5 introduces an internal worker pool for `Job` tasks. This prevents goroutine explosion when spawning many short-lived tasks.

- `WithJobPool(n)`: Sets the number of worker goroutines (default is `GOMAXPROCS`).
- `WithJobQueue(n)`: Sets the queue capacity.
- `Add(job)`: Blocks if the queue is full.
- `TryAdd(job)`: Returns `ErrJobQueueFull` immediately if the queue is full.
- `WithQueueFullPolicy(policy)`: Controls whether a full queue blocks, rejects, drops the oldest queued job, or drops the newest queued job.
- `WithJobPartition(name, config)`: Configures a named execution partition for jobs with dedicated worker/queue settings.

### 7. Priority-aware queued jobs

Pooled jobs can now express queue priority.

- `JobWithPriority(name, priority, runner)` creates a queued job with explicit priority.
- `Job(...).WithPriority(priority)` updates the priority on a task value.
- Higher priority values run first.
- Jobs with the same priority keep FIFO order.
- Queue-full policies still operate on queue age, not priority, so `DropOldest` and `DropNewest` keep their intuitive meaning.

### 8. Partitioned job execution

v5 introduces optional partitions for job execution. Each partition maintains its own independent queue and worker pool.

- `WithJobPartition(name, JobPartitionConfig{...})`: Configures a named partition.
- `JobInPartition(name, partition, runner)`: Creates a job targeted at a specific partition.
- `Job(...).WithPartition(partition)`: Sets the partition for an existing job.
- Jobs with no partition specified default to the `default` partition.
- Note: There is no global fairness or worker stealing between partitions in this version.

### 9. Explicit Task Kinds

In v4, all tasks were equal. In v5, you must choose between `Service` and `Job`:

- `Service`: Long-running loop. If it returns `nil` before the context is canceled, the runtime treats it as an error (`ErrUnexpectedServiceExit`).
- `Job`: Finite task. It is expected to return `nil` upon completion.

## Migration examples

### v4 style

```go
type Worker struct{}

func (Worker) Name() string { return "worker" }

func (Worker) Handle(ctx async.Context) {
	defer ctx.Exit()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// work
		}
	}
}

func (Worker) OnPreRun() {}
func (Worker) OnShutdown(context.Context) {}
```

### v5 style

```go
rt := async.NewRuntime()

// Service: long running
_ = rt.Add(async.Service("worker", func(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// work
		}
	}
}))

// Job: finite work
_ = rt.Add(async.Job("cleanup", func(ctx context.Context) error {
	// perform cleanup once
	return nil
}))
```

## Job pool and backpressure examples

For short-lived work, v5 can reuse worker goroutines. This is only applicable to `Job` tasks.

```go
rt := async.NewRuntime(
	async.WithJobPool(4),
	async.WithJobQueue(32),
	async.WithQueueFullPolicy(async.QueueFullReject),
)

err := rt.Add(async.JobWithPriority("task-1", 100, runTask))

err = rt.TryAdd(async.Job("task-2", runTask).WithPriority(10))
if errors.Is(err, async.ErrJobQueueFull) {
	// decide whether to retry, drop, or apply upstream backpressure
}

err = rt.Add(async.JobInPartition("heavy-task", "batch", runTask))
if errors.Is(err, async.ErrUnknownJobPartition) {
	// partition was not configured
}
```

## Recommended migration steps

1. Change imports to `github.com/dingdayu/async/v5`
2. Replace `NewAsync()` call sites with `NewRuntime()`
3. Replace `Handle` implementations with `Service(...)` or `Job(...)`
4. Replace `ctx.Exit()` with normal returns
5. Replace package-level runtime helpers with explicit runtime variables
6. Update signal handling to use `signal.NotifyContext`
7. Check all `Wait()` callers and handle returned errors

## Notes

- If you had long-running loop tasks in v4, migrate them to `Service`
- If you had startup/one-shot work in v4, migrate it to `Job`
- If you relied on global registration from many packages, introduce an application-owned runtime and inject it where needed
