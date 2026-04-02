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

func TestQueueFullRejectMakesAddReturnQueueFull(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(1), WithQueueFullPolicy(QueueFullReject))
	started := make(chan struct{})
	release := make(chan struct{})

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

	if err := rt.Add(Job("job-2", func(ctx context.Context) error { return nil })); err != nil {
		t.Fatalf("add job-2: %v", err)
	}

	err := rt.Add(Job("job-3", func(ctx context.Context) error { return nil }))
	if !errors.Is(err, ErrJobQueueFull) {
		t.Fatalf("expected ErrJobQueueFull, got %v", err)
	}

	close(release)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := rt.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestQueueFullDropOldestPrefersNewJob(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(1), WithQueueFullPolicy(QueueFullDropOldest))
	started := make(chan struct{})
	release := make(chan struct{})
	job2Ran := make(chan struct{})
	job3Ran := make(chan struct{})

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
		close(job2Ran)
		return nil
	})); err != nil {
		t.Fatalf("add job-2: %v", err)
	}

	if err := rt.TryAdd(Job("job-3", func(ctx context.Context) error {
		close(job3Ran)
		return nil
	})); err != nil {
		t.Fatalf("try add job-3: %v", err)
	}

	close(release)

	select {
	case <-job3Ran:
	case <-time.After(time.Second):
		t.Fatalf("job-3 did not run")
	}

	select {
	case <-job2Ran:
		t.Fatalf("expected job-2 to be dropped")
	case <-time.After(100 * time.Millisecond):
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

func TestQueueFullDropNewestDropsTailQueuedJob(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(2), WithQueueFullPolicy(QueueFullDropNewest))
	started := make(chan struct{})
	release := make(chan struct{})
	job2Ran := make(chan struct{})
	job3Ran := make(chan struct{})
	job4Ran := make(chan struct{})

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
		close(job2Ran)
		return nil
	})); err != nil {
		t.Fatalf("add job-2: %v", err)
	}

	if err := rt.Add(Job("job-3", func(ctx context.Context) error {
		close(job3Ran)
		return nil
	})); err != nil {
		t.Fatalf("add job-3: %v", err)
	}

	if err := rt.TryAdd(Job("job-4", func(ctx context.Context) error {
		close(job4Ran)
		return nil
	})); err != nil {
		t.Fatalf("try add job-4: %v", err)
	}

	close(release)

	select {
	case <-job2Ran:
	case <-time.After(time.Second):
		t.Fatalf("job-2 did not run")
	}

	select {
	case <-job4Ran:
	case <-time.After(time.Second):
		t.Fatalf("job-4 did not run")
	}

	select {
	case <-job3Ran:
		t.Fatalf("expected job-3 to be dropped")
	case <-time.After(100 * time.Millisecond):
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

func TestPooledJobsRunByPriorityThenFIFO(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(16))
	started := make(chan struct{})
	release := make(chan struct{})
	runOrder := make(chan string, 5)

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := rt.Add(Job("blocker", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return nil
		}
	})); err != nil {
		t.Fatalf("add blocker: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("blocker did not start")
	}

	add := func(task Task) {
		t.Helper()
		if err := rt.Add(task); err != nil {
			t.Fatalf("add %s: %v", task.Name, err)
		}
	}

	add(Job("low-1", func(ctx context.Context) error { runOrder <- "low-1"; return nil }))
	add(JobWithPriority("high-1", 100, func(ctx context.Context) error { runOrder <- "high-1"; return nil }))
	add(Job("low-2", func(ctx context.Context) error { runOrder <- "low-2"; return nil }))
	add(Job("mid", func(ctx context.Context) error { runOrder <- "mid"; return nil }).WithPriority(10))
	add(JobWithPriority("high-2", 100, func(ctx context.Context) error { runOrder <- "high-2"; return nil }))

	close(release)

	expected := []string{"high-1", "high-2", "mid", "low-1", "low-2"}
	for i, want := range expected {
		select {
		case got := <-runOrder:
			if got != want {
				t.Fatalf("run order[%d] = %q, want %q", i, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", want)
		}
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

func TestQueueFullDropOldestUsesAgeWithPriorityQueue(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(2), WithQueueFullPolicy(QueueFullDropOldest))
	started := make(chan struct{})
	release := make(chan struct{})
	jobOldHighRan := make(chan struct{})
	jobOldLowRan := make(chan struct{})
	jobNewMidRan := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := rt.Add(Job("blocker", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return nil
		}
	})); err != nil {
		t.Fatalf("add blocker: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("blocker did not start")
	}

	if err := rt.Add(JobWithPriority("old-high", 100, func(ctx context.Context) error {
		close(jobOldHighRan)
		return nil
	})); err != nil {
		t.Fatalf("add old-high: %v", err)
	}

	if err := rt.Add(JobWithPriority("old-low", 0, func(ctx context.Context) error {
		close(jobOldLowRan)
		return nil
	})); err != nil {
		t.Fatalf("add old-low: %v", err)
	}

	if err := rt.TryAdd(JobWithPriority("new-mid", 50, func(ctx context.Context) error {
		close(jobNewMidRan)
		return nil
	})); err != nil {
		t.Fatalf("try add new-mid: %v", err)
	}

	close(release)

	select {
	case <-jobNewMidRan:
	case <-time.After(time.Second):
		t.Fatalf("new-mid did not run")
	}

	select {
	case <-jobOldLowRan:
	case <-time.After(time.Second):
		t.Fatalf("old-low did not run")
	}

	select {
	case <-jobOldHighRan:
		t.Fatalf("expected old-high to be dropped as oldest queued job")
	case <-time.After(100 * time.Millisecond):
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

func TestJobPartitionsAreIsolated(t *testing.T) {
	rt := NewRuntime(
		WithJobPartition("alpha", JobPartitionConfig{Workers: 1, QueueCap: 4, QueueFullPolicy: QueueFullBlock}),
		WithJobPartition("beta", JobPartitionConfig{Workers: 1, QueueCap: 4, QueueFullPolicy: QueueFullBlock}),
	)

	alphaStarted := make(chan struct{})
	alphaRelease := make(chan struct{})
	betaRan := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := rt.Add(JobInPartition("alpha-blocker", "alpha", func(ctx context.Context) error {
		close(alphaStarted)
		select {
		case <-alphaRelease:
			return nil
		case <-ctx.Done():
			return nil
		}
	})); err != nil {
		t.Fatalf("add alpha blocker: %v", err)
	}

	select {
	case <-alphaStarted:
	case <-time.After(time.Second):
		t.Fatalf("alpha blocker did not start")
	}

	if err := rt.Add(JobInPartition("beta-job", "beta", func(ctx context.Context) error {
		close(betaRan)
		return nil
	})); err != nil {
		t.Fatalf("add beta job: %v", err)
	}

	select {
	case <-betaRan:
	case <-time.After(time.Second):
		t.Fatalf("beta partition job did not run while alpha was blocked")
	}

	close(alphaRelease)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := rt.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestUnknownJobPartitionIsRejected(t *testing.T) {
	rt := NewRuntime()

	err := rt.Add(JobInPartition("bad", "missing", func(ctx context.Context) error { return nil }))
	if !errors.Is(err, ErrUnknownJobPartition) {
		t.Fatalf("expected ErrUnknownJobPartition before start, got %v", err)
	}

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	err = rt.Add(JobInPartition("bad-running", "missing", func(ctx context.Context) error { return nil }))
	if !errors.Is(err, ErrUnknownJobPartition) {
		t.Fatalf("expected ErrUnknownJobPartition after start, got %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestPartitionDropAccountingStillShutsDown(t *testing.T) {
	rt := NewRuntime(
		WithJobPartition("slow", JobPartitionConfig{Workers: 1, QueueCap: 1, QueueFullPolicy: QueueFullDropOldest}),
	)

	started := make(chan struct{})
	release := make(chan struct{})
	oldRan := make(chan struct{})
	newRan := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := rt.Add(JobInPartition("slow-blocker", "slow", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return nil
		}
	})); err != nil {
		t.Fatalf("add blocker: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("blocker did not start")
	}

	if err := rt.Add(JobInPartition("old", "slow", func(ctx context.Context) error {
		close(oldRan)
		return nil
	})); err != nil {
		t.Fatalf("add old: %v", err)
	}

	if err := rt.TryAdd(JobInPartition("new", "slow", func(ctx context.Context) error {
		close(newRan)
		return nil
	})); err != nil {
		t.Fatalf("try add new: %v", err)
	}

	close(release)

	select {
	case <-newRan:
	case <-time.After(time.Second):
		t.Fatalf("new job did not run")
	}

	select {
	case <-oldRan:
		t.Fatalf("expected old job to be dropped")
	case <-time.After(100 * time.Millisecond):
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
