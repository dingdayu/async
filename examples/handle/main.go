package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

// ExampleAsync is a sample async task for demonstration.
type ExampleAsync struct{}

// Name returns the name of the ExampleAsync task.
func (a ExampleAsync) Name() string { return "example" }

// Handle runs the main logic of ExampleAsync.
func (a ExampleAsync) Handle(ctx async.Context) {
	defer ctx.Exit()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			fmt.Println("ExampleAsync: working")
			time.Sleep(2 * time.Second)
		}
	}
}

// OnPreRun is called before ExampleAsync starts running.
func (a ExampleAsync) OnPreRun() {}

// OnShutdown is called when ExampleAsync is shutting down.
func (a ExampleAsync) OnShutdown(ctx context.Context) { fmt.Println("ExampleAsync: shutdown") }

func main() {
	a := async.NewAsync()
	if err := a.Register(ExampleAsync{}); err != nil {
		panic(err)
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Run starts the manager and waits until ExampleAsync calls ctx.Exit().
	if err := a.Run(runCtx); err != nil {
		panic(err)
	}
	fmt.Println("all tasks exited")
}
