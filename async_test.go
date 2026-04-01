package async

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunJobCompletion(t *testing.T) {
	rt := NewRuntime()
	var ran int32
	if err := rt.Add(Job("job", func(ctx context.Context) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})); err != nil {
		t.Fatalf("add job: %v", err)
	}

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if atomic.LoadInt32(&ran) != 1 {
		t.Fatalf("job did not run exactly once")
	}
}

func TestServiceShutdown(t *testing.T) {
	rt := NewRuntime()
	started := make(chan struct{})
	stopped := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("service did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatalf("service did not stop")
	}
}

func TestFailurePropagationFailFast(t *testing.T) {
	rt := NewRuntime()
	stopped := make(chan struct{})
	boom := errors.New("boom")

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Add(Job("bad", func(ctx context.Context) error {
		return boom
	})); err != nil {
		t.Fatalf("add job: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	err := rt.Wait()
	if err == nil {
		t.Fatalf("expected failure")
	}

	var taskErr *TaskError
	if !errors.As(err, &taskErr) {
		t.Fatalf("expected TaskError, got %T", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom")
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatalf("service was not canceled")
	}
}

func TestDynamicAddWhileRunning(t *testing.T) {
	rt := NewRuntime()
	started := make(chan struct{})
	jobDone := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("service did not start")
	}

	if err := rt.Add(Job("later", func(ctx context.Context) error {
		close(jobDone)
		return nil
	})); err != nil {
		t.Fatalf("add dynamic job: %v", err)
	}

	select {
	case <-jobDone:
	case <-time.After(time.Second):
		t.Fatalf("dynamic job did not finish")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestPanicIsFailure(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Add(Job("panic", func(ctx context.Context) error {
		panic("bad panic")
	})); err != nil {
		t.Fatalf("add panic job: %v", err)
	}

	err := rt.Run(context.Background())
	if err == nil {
		t.Fatalf("expected panic failure")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected panic text, got %v", err)
	}
}

func TestServiceUnexpectedExit(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	err := rt.Run(context.Background())
	if err == nil {
		t.Fatalf("expected service failure")
	}
	if !errors.Is(err, ErrUnexpectedServiceExit) {
		t.Fatalf("expected ErrUnexpectedServiceExit, got %v", err)
	}
}

func TestJobPoolLimitsConcurrency(t *testing.T) {
	rt := NewRuntime(WithJobPool(1))
	var current int32
	var maxSeen int32

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("job-%d", i)
		if err := rt.Add(Job(name, func(ctx context.Context) error {
			now := atomic.AddInt32(&current, 1)
			for {
				max := atomic.LoadInt32(&maxSeen)
				if now <= max || atomic.CompareAndSwapInt32(&maxSeen, max, now) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			return nil
		})); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := atomic.LoadInt32(&maxSeen); got != 1 {
		t.Fatalf("expected pooled jobs to run serially with one worker, got max concurrency %d", got)
	}
	if got := atomic.LoadInt32(&current); got != 0 {
		t.Fatalf("expected no running jobs, got %d", got)
	}
}

func TestTryAddReturnsQueueFullForPooledJobs(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(1))
	started := make(chan struct{})
	release := make(chan struct{})
	queuedDone := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := rt.Add(Job("job-1", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return nil
		}
	})); err != nil {
		t.Fatalf("add job-1: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("job-1 did not start")
	}

	if err := rt.Add(Job("job-2", func(ctx context.Context) error {
		close(queuedDone)
		return nil
	})); err != nil {
		t.Fatalf("add job-2: %v", err)
	}

	err := rt.TryAdd(Job("job-3", func(ctx context.Context) error { return nil }))
	if !errors.Is(err, ErrJobQueueFull) {
		t.Fatalf("expected ErrJobQueueFull, got %v", err)
	}

	close(release)

	select {
	case <-queuedDone:
	case <-time.After(time.Second):
		t.Fatalf("queued job did not finish")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := rt.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestAddBlocksUntilQueueHasCapacity(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(1))
	started := make(chan struct{})
	release := make(chan struct{})
	job2Done := make(chan struct{})
	job3Done := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := rt.Add(Job("job-1", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return nil
		}
	})); err != nil {
		t.Fatalf("add job-1: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("job-1 did not start")
	}

	if err := rt.Add(Job("job-2", func(ctx context.Context) error {
		close(job2Done)
		return nil
	})); err != nil {
		t.Fatalf("add job-2: %v", err)
	}

	addDone := make(chan error, 1)
	go func() {
		addDone <- rt.Add(Job("job-3", func(ctx context.Context) error {
			close(job3Done)
			return nil
		}))
	}()

	select {
	case err := <-addDone:
		t.Fatalf("expected add to block, returned early with %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-addDone:
		if err != nil {
			t.Fatalf("add job-3: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("add did not unblock after queue space was available")
	}

	select {
	case <-job2Done:
	case <-time.After(time.Second):
		t.Fatalf("job-2 did not finish")
	}

	select {
	case <-job3Done:
	case <-time.After(time.Second):
		t.Fatalf("job-3 did not finish")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := rt.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}
