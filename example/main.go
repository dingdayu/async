package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dingdayu/async/v3"
)

type ExampleAsync struct {
}

// OnPreRun Before run, panic panic causes registration failure
func (a ExampleAsync) OnPreRun() {
	fmt.Printf("\u001B[1;30;42m[info]\u001B[0m ExampleAsync 注册成功，开始运行！\n")
}

// Name async name
func (a ExampleAsync) Name() string {
	return "example"
}

// Handle async logical
func (a ExampleAsync) Handle(ctx async.Context) {
	defer ctx.Exit()

	for {
		select {
		default:
			// todo:: Logical unit
			time.Sleep(3 * time.Second)
			fmt.Println("ExampleAsync", time.Now().Format("2006-01-02 15:04:05"))
		case <-ctx.Done():
			return
		}
	}
}

// OnShutdown on async shutdown
func (a ExampleAsync) OnShutdown(s os.Signal) {
	fmt.Printf("\u001B[1;30;42m[info]\u001B[0m ExampleAsync 接收到系统信号[%s]，准备退出！\n", s.String())
}

type ExitAsync struct {
}

// OnPreRun Before run, panic panic causes registration failure
func (a ExitAsync) OnPreRun() {
	fmt.Printf("\u001B[1;30;42m[info]\u001B[0m ExitAsync 注册成功，开始运行！\n")
}

// Name async name
func (a ExitAsync) Name() string {
	return "exit_async"
}

// Handle async logical
func (a ExitAsync) Handle(ctx async.Context) {
	defer ctx.Exit()

	i := 10
	for {
		select {
		default:
			// todo:: Logical unit
			time.Sleep(1 * time.Second)
			fmt.Println("ExitAsync", time.Now().Format("2006-01-02 15:04:05"))
			if i < 0 {
				fmt.Println("ExitAsync 主动退出")
				return
			}
			i--
		case <-ctx.Done():
			return
		}
	}
}

// OnShutdown on async shutdown
func (a ExitAsync) OnShutdown(s os.Signal) {
	fmt.Printf("\u001B[1;30;42m[info]\u001B[0m ExitAsync 接收到系统信号[%s]，准备退出！\n", s.String())
}

func main() {
	// Handle SIGINT and SIGTERM.
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	ayc := async.NewAsync(context.Background(), ch)

	_ = ayc.Register(ExampleAsync{})
	_ = ayc.Register(ExitAsync{})

	ayc.Wait()
	fmt.Println("[1;30;42m[info]\u001B[0m Task exited")
}
