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
	a := async.NewAsync(context.Background())

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

	_ = a.Register(t)
	a.Wait()
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
// a := async.NewAsync(context.Background())
// _ = a.Register(MyHandle{})
```

## Examples

See the `examples/` folder for two separate runnable examples:

- `examples/handle`: a `main.go` that demonstrates implementing `Handle` directly.
- `examples/task`: a `main.go` that demonstrates using `NewTask` and its callbacks.

Run them with:

```bash
# run the handle example
go run ./examples/handle

# run the task example
go run ./examples/task
```

Why use `Task` vs `Handle`?

- `Task` is a convenience struct for quick tasks. It reduces boilerplate when you only need a simple run loop and optional hooks.
- `Handle` (interface) is more flexible for complex tasks that require internal state, methods, or embedding.

Choose `Task` for quick prototypes and `Handle` when you need full control.
