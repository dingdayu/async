# async

Go 的安全异步任务管理器。

这个库用于在 Go 中统一管理**长时间运行的后台服务**与**有限生命周期的任务（Job）**。v5 版本将旧的 Handle 风格 API 重构为围绕命名任务与 `context.Context` 的显式 Runtime 模型。

## 安装

```bash
go get github.com/dingdayu/async/v5
```

## 快速开始

v5 的最小执行原语是：

- `async.Runner`：`func(context.Context) error`

然后你可以将 Runner 封装为两类任务：

- `async.Service(...)`：长时间运行任务，通常持续到系统关闭
- `async.Job(...)`：有限任务，执行完成后返回
- `async.JobWithPriority(...)`（或 `async.Job(...).WithPriority(...)`）：有限任务，支持优先级排队
- `async.JobInPartition(...)`（或 `async.Job(...).WithPartition(...)`）：将有限任务路由到指定执行分区

### Runtime

创建一个显式 Runtime，添加任务后：

- 使用 `Run` 阻塞运行，或
- 使用 `Start` + `Shutdown` + `Wait` 手动管理生命周期

**任务语义：**

- `async.Service("name", runner)`：长运行任务；如果在 context 未取消前返回 `nil`，会被视为错误（`ErrUnexpectedServiceExit`）
- `async.Job("name", runner)`：有限任务；正常完成时应返回 `nil`

**Job 池与背压：**

默认情况下，Job 通过内部 worker 池执行。对突发短任务来说，这通常比每个任务都创建新 goroutine 更高效。

- `WithJobPool(n)`：设置 worker 数（默认 `GOMAXPROCS`）；设为 0 表示关闭池化
- `WithJobQueue(n)`：设置队列容量；默认 0（不设上限）
- `Add(task)`：当 pooled Job 队列满时会阻塞等待容量
- `TryAdd(task)`：当队列饱和时立即返回 `async.ErrJobQueueFull`
- `WithQueueFullPolicy(policy)`：设置有界队列满时策略
  - `QueueFullBlock`（默认）：`Add` 阻塞，`TryAdd` 返回 `ErrJobQueueFull`
  - `QueueFullReject`：`Add` 与 `TryAdd` 均返回 `ErrJobQueueFull`
  - `QueueFullDropOldest`：丢弃最老排队任务，再入队新任务
  - `QueueFullDropNewest`：丢弃最新排队任务，再入队新任务
- `WithJobPartition(name, config)`：配置命名执行分区；每个分区拥有独立 worker 池与队列
- pooled Job 队列支持优先级：`Task.Priority` 越高越先执行；同优先级保持 FIFO

**典型用法：**

```go
import (
	"context"
	async "github.com/dingdayu/async/v5"
)

rt := async.NewRuntime()

_ = rt.Add(async.Service("worker", func(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// do work
		}
	}
}))

_ = rt.Add(async.Job("warm-cache", func(ctx context.Context) error {
	// 有限启动任务
	return nil
}))

if err := rt.Run(context.Background()); err != nil {
	panic(err)
}
```

### 有界队列 Job 示例

```go
rt := async.NewRuntime(
	async.WithJobPool(4),
	async.WithJobQueue(32),
	async.WithQueueFullPolicy(async.QueueFullReject),
)

err := rt.TryAdd(async.Job("send-email", func(ctx context.Context) error {
	// 短任务
	return nil
}))
if errors.Is(err, async.ErrJobQueueFull) {
	// 可选择重试、丢弃或向上游施加背压
}
```

### Job 分区（Partition）

分区可用于隔离不同类型的后台工作。例如可以让“关键任务”分区拥有更多 worker，“批处理”分区拥有更少 worker。

```go
rt := async.NewRuntime(
	async.WithJobPartition("batch", async.JobPartitionConfig{
		Workers:  2,
		QueueCap: 100,
	}),
)

// 路由到分区
_ = rt.Add(async.JobInPartition("process-video", "batch", func(ctx context.Context) error {
	return nil
}))

// 或通过 WithPartition 设置
_ = rt.Add(async.Job("generate-report", func(ctx context.Context) error {
	return nil
}).WithPartition("batch"))
```

默认情况下，Job 运行在 `default` 分区。全局配置（如 `WithJobPool` / `WithJobQueue`）会应用到 `default` 分区。

> 注意：当前分区实现不支持跨分区 worker stealing，也不提供全局公平调度；各分区队列与 worker 独立。

**何时使用 Service：**

- 消费者、轮询器、流处理、后台同步循环
- 需要持续运行直到系统关闭的任务

**何时使用 Job：**

- 预热任务、迁移、一类一次性后台任务、启动探测
- 有限执行并返回的任务

### Service 示例

```go
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
				return nil
			default:
				fmt.Println("task running")
				time.Sleep(1 * time.Second)
			}
		}
	})); err != nil {
		panic(err)
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rt.Run(runCtx); err != nil {
		panic(err)
	}
}
```

### 手动生命周期控制

```go
rt := async.NewRuntime()
_ = rt.Add(async.Service("worker", runWorker))

parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := rt.Start(parentCtx); err != nil {
	panic(err)
}

<-parentCtx.Done()

shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := rt.Shutdown(shutdownCtx); err != nil {
	panic(err)
}

if err := rt.Wait(); err != nil {
	panic(err)
}
```

## 示例

`examples/` 目录包含可运行的 v5 示例：

- `examples/handle`：长运行 Service 示例
- `examples/task`：显式 shutdown 的 runtime 示例
- `examples/default`：混合 Job 与 Service 的 runtime 示例

运行方式：

```bash
go run ./examples/handle
go run ./examples/task
go run ./examples/default
```

## v4 到 v5 迁移说明（摘要）

- 核心 API 移除了 `Handle` / `Context.Exit()`
- 移除了包级全局注册模型
- 任务清理建议使用 runner 内部 `defer`
- `Wait()` 现在返回首个任务错误
- 长运行任务建议建模为 `Service`，有限任务建模为 `Job`

v5 的主要目标是更清晰的生命周期语义、错误传播与显式 runtime 所有权。

完整迁移文档见：[MIGRATION_v5.md](MIGRATION_v5.md)

## 开发

项目使用 `Makefile` 管理常用命令：

- 运行测试：`make test`
- 运行 lint：`make lint`
- 运行 benchmark：`make bench`
- 运行 examples：`make examples`

等价原生命令：

```bash
go test ./...
golangci-lint run
go test ./... -run '^$' -bench 'BenchmarkRuntime' -benchmem
go run ./examples/default
```

当前 benchmark 重点覆盖：

- 普通 Job 与 pooled Job 的执行对比
- 优先级队列插入开销
- 分区提交开销
- 分区隔离压力场景

## 贡献

欢迎贡献！请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 了解开发流程与 PR 提交流程。

## 发布

项目使用 [GoReleaser](https://goreleaser.com/) 发布，可本地通过以下命令做快照验证：

```bash
make release-snapshot
```

当前进行中的破坏性开发主线为 `v5`。
