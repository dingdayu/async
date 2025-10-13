package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

// DemoTask is a sample async task for demonstration.
type DemoTask struct{}

// Name returns the name of the DemoTask.
func (d DemoTask) Name() string { return "demo" }

// Handle runs the main logic of DemoTask.
func (d DemoTask) Handle(ctx async.Context) {
	defer ctx.Exit()
	for i := 0; i < 3; i++ {
		fmt.Println("DemoTask running", i)
		time.Sleep(1 * time.Second)
	}
}

// OnPreRun is called before DemoTask starts running.
func (d DemoTask) OnPreRun() {}

// OnShutdown is called when DemoTask is shutting down.
func (d DemoTask) OnShutdown(ctx context.Context) { fmt.Println("DemoTask shutdown") }

func main() {
	_ = async.Register(DemoTask{})
	_ = async.Register(async.NewTask("quick", func(ctx async.Context) {
		defer ctx.Exit()
		fmt.Println("Quick task running")
	}))
	if err := async.Run(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println("DefaultAsync example exited")
}
