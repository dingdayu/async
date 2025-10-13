# async

Safe asynchronous tasks manager for Go.

This small library helps you run multiple background tasks (called "handles" or "tasks"), coordinate shutdown, and execute hooks safely. It provides a lightweight interface-based API for advanced control and a convenient `Task` struct for quick use (like `cobra.Command` style convenience).

## Install

```bash
go get github.com/dingdayu/async/v4
```

## Quick start

There are two simple ways to define a task:

- Implement the `Handle` interface (advanced/flexible).
- Use the provided `Task` struct and callbacks (convenient, fewer lines).

### DefaultAsync: Global Task Registration

For convenience, async provides a global instance `DefaultAsync` and package-level functions `Register`, `Start`, `Run`, and `Wait`.
This allows you to register tasks from anywhere in your project, even across multiple packages, and manage them centrally—similar to `prometheus.DefaultRegisterer`.

**Typical usage:**

```go
import "github.com/dingdayu/async/v4"

// In any package:
async.Register(MyHandle{})
async.Register(async.NewTask("quick", func(ctx async.Context) { /* ... */ }))

// In your main, either block:
if err := async.Run(context.Background()); err != nil {
	panic(err)
}
// or start asynchronously:
stop, err := async.Start(context.Background())
if err != nil {
	panic(err)
}
defer stop()
async.Wait()
```

**When to use:**

- You want to register tasks from multiple packages/modules and manage them together.
- You prefer not to manually manage Async instances.
- You want a simple, global entry point for background jobs.

See `examples/default/main.go` for a runnable demo.

### Using `Task` (recommended for most users)

```go
package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

func main() {
	a := async.NewAsync()

	// create a simple Task from callbacks
	t := async.NewTask("example", func(ctx async.Context) {
		defer ctx.Exit()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				fmt.Println("task running")
				time.Sleep(1 * time.Second)
			}
		}
	}, async.WithTaskPreRun(func(){ fmt.Println("pre-run") }), async.WithTaskShutdown(func(ctx context.Context){ fmt.Println("shutdown") }))

	if err := a.Register(t); err != nil {
		panic(err)
	}

	// Run blocks until all registered handles exit, so no separate Wait call is required.
	if err := a.Run(context.Background()); err != nil {
		panic(err)
	}
}
```

### Implementing `Handle` directly (advanced)

```go
type MyHandle struct{}
func (h MyHandle) Name() string { return "my" }
func (h MyHandle) Handle(ctx async.Context) { /* run loop and call ctx.Exit() to stop */ }
func (h MyHandle) OnPreRun() { /* optional */ }
func (h MyHandle) OnShutdown(ctx context.Context) { /* cleanup */ }

// register
// a := async.NewAsync()
// _ = a.Register(MyHandle{})
// _ = a.Run(ctx) // or call async.Start(ctx) + async.Wait() for asynchronous control
```

## Examples

See the `examples/` folder for three separate runnable examples:

- `examples/handle`: a `main.go` that demonstrates implementing `Handle` directly.
- `examples/task`: a `main.go` that demonstrates using `NewTask` and its callbacks.
- `examples/default`: a `main.go` that demonstrates registering tasks to the global `DefaultAsync` from any package.

Run them with:

```bash
# run the handle example
go run ./examples/handle

# run the task example
go run ./examples/task

# run the DefaultAsync example
go run ./examples/default
```

Why use `Task` vs `Handle`?

- `Task` is a convenience struct for quick tasks. It reduces boilerplate when you only need a simple run loop and optional hooks.
- `Handle` (interface) is more flexible for complex tasks that require internal state, methods, or embedding.
- `DefaultAsync` lets you register tasks globally from anywhere, making it easy to coordinate background jobs across packages.

Choose `Task` for quick prototypes, `Handle` for full control, and `DefaultAsync` for global registration and coordination.
