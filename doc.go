// Package async provides explicit runtime orchestration for long-running services
// and finite jobs.
//
// The v5 API centers on Runtime and Runner-based tasks:
//
//	rt := async.NewRuntime()
//	_ = rt.Add(async.Service("worker", func(ctx context.Context) error {
//		<-ctx.Done()
//		return nil
//	}))
//	if err := rt.Run(ctx); err != nil { /* handle error */ }
//
// Services model long-running workers that should stay alive until shutdown.
// Jobs model finite work that completes by returning.
// Control options like WithJobPool, WithJobQueue, WithQueueFullPolicy, and
// WithJobPartition shape scheduling, while WithTaskMiddleware, Runtime.Stats,
// and WithObserver expose extension and observability hooks without changing
// task code.
//
// See the examples/ directory and README for more samples.
package async
