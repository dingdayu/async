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
	if err := rt.Add(async.Job("quick", func(ctx context.Context) error {
		fmt.Println("Quick task running")
		return nil
	})); err != nil {
		panic(err)
	}
	if err := rt.Add(async.Job("demo", func(ctx context.Context) error {
		for i := 0; i < 3; i++ {
			fmt.Println("DemoTask running", i)
			time.Sleep(1 * time.Second)
		}
		return nil
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

	fmt.Println("runtime example exited")
}
