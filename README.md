# async

Safe asynchronous tasks by Go.

## Install 

```bash
go get github.com/dingdayu/async
```

## Example

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "sync"
    "syscall"

    "github.com/dingdayu/async"
)

func main()  {
    // Handle SIGINT and SIGTERM.
    ch := make(chan os.Signal)
    signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

    ayc := async.NewAsync(context.Background(), ch)
    // 注册 定时器的处理器
    ayc.Register(func(ctx2 context.Context, wg *sync.WaitGroup) {
        t := timer.NewTimer(redis.Redis())
        t.Handle(ctx, wg)
    })

    ayc.Wait()
    fmt.Println("完全退出！")
}
```