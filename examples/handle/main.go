package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

type ExampleAsync struct{}

func (a ExampleAsync) Name() string { return "example" }
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
func (a ExampleAsync) OnPreRun()                      {}
func (a ExampleAsync) OnShutdown(ctx context.Context) { fmt.Println("ExampleAsync: shutdown") }

func main() {
	a := async.NewAsync(context.Background())
	_ = a.Register(ExampleAsync{})
	a.Wait()
	fmt.Println("all tasks exited")
}
