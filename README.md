# async

Safe asynchronous tasks by Go.

Correctly handle internal exits in v3.

## Install 

```bash
go get github.com/dingdayu/async/v3
```

## Example

```go
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

func main() {
	// Handle SIGINT and SIGTERM.
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	ayc := async.NewAsync(context.Background(), ch)

	_ = ayc.Register(ExampleAsync{})

	ayc.Wait()
	fmt.Println("[1;30;42m[info]\u001B[0m Task exited")
}

```