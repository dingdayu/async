package async

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func BenchmarkRuntimeRunJobs(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rt := NewRuntime(WithJobPool(0))
		for j := 0; j < 64; j++ {
			if err := rt.Add(Job(fmt.Sprintf("job-%d", j), func(ctx context.Context) error {
				return nil
			})); err != nil {
				b.Fatalf("add job: %v", err)
			}
		}
		if err := rt.Run(context.Background()); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

func BenchmarkRuntimeRunPooledJobs(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rt := NewRuntime(WithJobPool(4), WithJobQueue(128))
		for j := 0; j < 128; j++ {
			if err := rt.Add(Job(fmt.Sprintf("job-%d", j), func(ctx context.Context) error {
				return nil
			})); err != nil {
				b.Fatalf("add job: %v", err)
			}
		}
		if err := rt.Run(context.Background()); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

func BenchmarkRuntimePriorityQueueSubmission(b *testing.B) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(0))
	blockerRelease := make(chan struct{})
	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		b.Fatalf("add service: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		b.Fatalf("start: %v", err)
	}
	if err := rt.Add(Job("blocker", func(ctx context.Context) error {
		<-blockerRelease
		return nil
	})); err != nil {
		b.Fatalf("add blocker: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rt.Add(Job(fmt.Sprintf("job-%d", i), func(ctx context.Context) error {
			return nil
		}).WithPriority(i % 8)); err != nil {
			b.Fatalf("add priority job: %v", err)
		}
	}
	b.StopTimer()

	close(blockerRelease)
	if err := rt.Shutdown(context.Background()); err != nil {
		b.Fatalf("shutdown: %v", err)
	}
	if err := rt.Wait(); err != nil {
		b.Fatalf("wait: %v", err)
	}
}

func BenchmarkRuntimePartitionedSubmission(b *testing.B) {
	rt := NewRuntime(
		WithJobPartition("alpha", JobPartitionConfig{Workers: 2, QueueCap: 0, QueueFullPolicy: QueueFullBlock}),
		WithJobPartition("beta", JobPartitionConfig{Workers: 2, QueueCap: 0, QueueFullPolicy: QueueFullBlock}),
	)
	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		b.Fatalf("add service: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		b.Fatalf("start: %v", err)
	}

	b.ResetTimer()
	var seq int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := atomic.AddInt64(&seq, 1)
			partition := "alpha"
			if id%2 == 1 {
				partition = "beta"
			}
			name := fmt.Sprintf("job-%d", id)
			if err := rt.Add(JobInPartition(name, partition, func(ctx context.Context) error {
				return nil
			})); err != nil {
				b.Fatalf("add partitioned job: %v", err)
			}
		}
	})
	b.StopTimer()

	if err := rt.Shutdown(context.Background()); err != nil {
		b.Fatalf("shutdown: %v", err)
	}
	if err := rt.Wait(); err != nil {
		b.Fatalf("wait: %v", err)
	}
}

func BenchmarkRuntimePartitionIsolationPressure(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rt := NewRuntime(
			WithJobPartition("latency", JobPartitionConfig{Workers: 1, QueueCap: 128, QueueFullPolicy: QueueFullBlock}),
			WithJobPartition("bulk", JobPartitionConfig{Workers: 1, QueueCap: 128, QueueFullPolicy: QueueFullBlock}),
		)
		if err := rt.Add(Service("svc", func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		})); err != nil {
			b.Fatalf("add service: %v", err)
		}
		if err := rt.Start(context.Background()); err != nil {
			b.Fatalf("start: %v", err)
		}

		var ran int32
		for j := 0; j < 64; j++ {
			if err := rt.Add(JobInPartition(fmt.Sprintf("bulk-%d", j), "bulk", func(ctx context.Context) error {
				atomic.AddInt32(&ran, 1)
				return nil
			})); err != nil {
				b.Fatalf("add bulk job: %v", err)
			}
		}
		if err := rt.Add(JobInPartition("latency-job", "latency", func(ctx context.Context) error {
			atomic.AddInt32(&ran, 1)
			return nil
		})); err != nil {
			b.Fatalf("add latency job: %v", err)
		}
		if err := rt.Shutdown(context.Background()); err != nil {
			b.Fatalf("shutdown: %v", err)
		}
		if err := rt.Wait(); err != nil {
			b.Fatalf("wait: %v", err)
		}
		if got := atomic.LoadInt32(&ran); got != 65 {
			b.Fatalf("expected 65 jobs to run, got %d", got)
		}
	}
}
