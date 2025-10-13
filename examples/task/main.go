package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

func main() {
	ay := async.NewAsync(context.Background())

	t := async.NewTask("task1", func(ctx async.Context) {
		defer ctx.Exit()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				fmt.Println("Task running")
				time.Sleep(500 * time.Millisecond)
			}
		}
	}, async.WithTaskPreRun(func() { fmt.Println("Task pre-run") }), async.WithTaskShutdown(func(ctx context.Context) { fmt.Println("Task shutdown") }))

	_ = ay.Register(t)

	ay.Wait()
	fmt.Println("task example exited")
}
