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
	if err := rt.Add(async.Service("task1", func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				fmt.Println("Task shutdown")
				return nil
			default:
				fmt.Println("Task running")
				time.Sleep(500 * time.Millisecond)
			}
		}
	})); err != nil {
		panic(err)
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithTimeout(signalCtx, 3*time.Second)
	defer cancel()

	if err := rt.Run(runCtx); err != nil {
		panic(err)
	}

	fmt.Println("task example exited")
}
