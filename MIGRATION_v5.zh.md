# 从 v4 迁移到 v5

v5 是 `async` 的破坏性重构版本。

项目不再围绕 `Handle`、`Context.Exit()` 与包级默认 runtime，而是改为基于 `func(context.Context) error` 的显式 `Runtime` + `Service` / `Job` 模型。

## 发布摘要

### 主要变化

- 模块路径变更为 `github.com/dingdayu/async/v5`
- 核心抽象改为 `Runtime`
- 核心 API 移除 `Handle` 与 `Context`
- 核心 API 移除包级全局 runtime 辅助函数
- `Wait()` 现在返回任务/运行时错误
- `Service` 与 `Job` 的退出语义分离
- 短任务可通过内部 worker 池执行
- 通过 `WithJobQueue(...)` 与 `TryAdd(...)` 提供有界队列背压
- pooled Job 支持优先级与 queue-full 策略

### 保持不变的概念

- 仍然是注册命名后台任务
- 仍然在父级 `context.Context` 下运行
- 仍然通过 cancel 父 context 或调用 `Shutdown(ctx)` 完成优雅关闭

## API 映射

| v4 | v5 |
|---|---|
| `NewAsync()` | `NewRuntime()` |
| `Handle` | `Service(...)` 或 `Job(...)` |
| `Handle(ctx async.Context)` | `func(context.Context) error` |
| `ctx.Exit()` | `return nil` |
| `async.Register(...)` | `rt.Add(...)` |
| `async.Run(ctx)` | `rt.Run(ctx)` |
| `async.Start(ctx)` | `rt.Start(ctx)` |
| `async.Wait()` | `rt.Wait()` |
| 包级默认 runtime | 显式 runtime 所有权 |

## 概念变化

### 1. 显式 runtime 所有权

v4 倾向包级默认 runtime；v5 要求你显式创建并持有 runtime：

```go
rt := async.NewRuntime()
```

这样可以减少全局隐式耦合，并明确生命周期归属。

### 2. `Handle` 改为 `Service` / `Job`

v4 中任务形态不区分“长运行”与“有限任务”。

v5 中：

- `Service`：长运行任务
- `Job`：有限任务

这让 runtime 可以把 Service 的异常早退识别为错误，同时允许 Job 正常完成退出。

### 3. `ctx.Exit()` 移除

v4 通过自定义 context 显式退出。

v5 中任务完成就是函数返回：

```go
return nil
```

### 4. shutdown 显式化

v5 核心 runtime 不再内置信号处理；应用层自己接管：

```go
signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if err := rt.Run(signalCtx); err != nil {
	panic(err)
}
```

### 5. `Wait()` 返回错误

`Wait()` 现在返回首个任务失败，使其和 `Run()` 的错误语义一致。

### 6. Job 池与背压

v5 为 `Job` 引入内部 worker 池，避免大量短任务导致 goroutine 爆炸。

- `WithJobPool(n)`：设置 worker 数（默认 `GOMAXPROCS`）
- `WithJobQueue(n)`：设置队列容量
- `Add(job)`：队列满时阻塞
- `TryAdd(job)`：队列满时立即返回 `ErrJobQueueFull`
- `WithQueueFullPolicy(policy)`：队列满策略（阻塞、拒绝、丢最老、丢最新）
- `WithJobPartition(name, config)`：为 Job 配置命名分区（独立 worker/queue）

### 7. 优先级队列 Job

pooled Job 支持优先级：

- `JobWithPriority(name, priority, runner)`
- `Job(...).WithPriority(priority)`
- priority 高者先执行
- 同优先级保持 FIFO
- queue-full 策略仍按“队列年龄”语义工作，不被 priority 改写

### 8. 分区 Job 执行

v5 引入可选 Job 分区。每个分区有独立队列与 worker 池：

- `WithJobPartition(name, JobPartitionConfig{...})`
- `JobInPartition(name, partition, runner)`
- `Job(...).WithPartition(partition)`
- 未指定分区的 Job 默认进 `default`
- 当前版本不支持跨分区 worker stealing / 全局公平调度

### 9. 任务类型显式化

v5 必须显式区分 `Service` 与 `Job`：

- `Service`：长循环任务；若在 context 取消前返回 `nil`，会被视为错误（`ErrUnexpectedServiceExit`）
- `Job`：有限任务；正常完成应返回 `nil`

## 迁移示例

### v4 风格

```go
type Worker struct{}

func (Worker) Name() string { return "worker" }

func (Worker) Handle(ctx async.Context) {
	defer ctx.Exit()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// work
		}
	}
}

func (Worker) OnPreRun() {}
func (Worker) OnShutdown(context.Context) {}
```

### v5 风格

```go
rt := async.NewRuntime()

// Service: 长运行
_ = rt.Add(async.Service("worker", func(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// work
		}
	}
}))

// Job: 有限任务
_ = rt.Add(async.Job("cleanup", func(ctx context.Context) error {
	return nil
}))
```

## Job 池与背压示例

```go
rt := async.NewRuntime(
	async.WithJobPool(4),
	async.WithJobQueue(32),
	async.WithQueueFullPolicy(async.QueueFullReject),
)

err := rt.Add(async.JobWithPriority("task-1", 100, runTask))

err = rt.TryAdd(async.Job("task-2", runTask).WithPriority(10))
if errors.Is(err, async.ErrJobQueueFull) {
	// 可选择重试、丢弃或向上游施加背压
}

err = rt.Add(async.JobInPartition("heavy-task", "batch", runTask))
if errors.Is(err, async.ErrUnknownJobPartition) {
	// 分区未配置
}
```

## 推荐迁移步骤

1. 将 import 改为 `github.com/dingdayu/async/v5`
2. 用 `NewRuntime()` 替换 `NewAsync()`
3. 将 `Handle` 实现迁移为 `Service(...)` / `Job(...)`
4. 用普通 `return` 替换 `ctx.Exit()`
5. 用显式 runtime 变量替代包级全局注册模型
6. 将信号处理改为 `signal.NotifyContext`
7. 检查所有 `Wait()` 调用方并处理返回错误

## 备注

- v4 的长循环任务建议迁移为 `Service`
- v4 的一次性任务建议迁移为 `Job`
- 如果之前依赖跨包全局注册，请改为应用层持有 runtime 并按需注入
