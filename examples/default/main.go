package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

// This example shows registering tasks from different packages into DefaultAsync

type DemoTask struct{}

func (d DemoTask) Name() string { return "demo" }
func (d DemoTask) Handle(ctx async.Context) {
	defer ctx.Exit()
	for i := 0; i < 3; i++ {
		fmt.Println("DemoTask running", i)
		time.Sleep(1 * time.Second)
	}
}
func (d DemoTask) OnPreRun()                      {}
func (d DemoTask) OnShutdown(ctx context.Context) { fmt.Println("DemoTask shutdown") }

func main() {
	async.Register(DemoTask{})
	async.Register(async.NewTask("quick", func(ctx async.Context) {
		defer ctx.Exit()
		fmt.Println("Quick task running")
	}))
	async.Wait()
	fmt.Println("DefaultAsync example exited")
}
