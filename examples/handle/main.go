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
	if err := rt.Add(async.Service("example", func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				fmt.Println("ExampleAsync: shutdown")
				return nil
			default:
				fmt.Println("ExampleAsync: working")
				time.Sleep(2 * time.Second)
			}
		}
	})); err != nil {
		panic(err)
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithTimeout(signalCtx, 5*time.Second)
	defer cancel()

	if err := rt.Run(runCtx); err != nil {
		panic(err)
	}

	fmt.Println("all tasks exited")
}
