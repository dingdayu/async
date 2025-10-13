package main

import (
	"context"
	"fmt"
	"time"

	async "github.com/dingdayu/async/v4"
)

func main() {
	ay := async.NewAsync()
	stop, err := ay.Start(context.Background())
	if err != nil {
		panic(err)
	}
	defer stop()

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

	if err := ay.Register(t); err != nil {
		panic(err)
	}

	ay.Wait()
	fmt.Println("task example exited")
}
