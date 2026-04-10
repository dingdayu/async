package async

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func (o *recordingObserver) Observe(event Event) {
	o.mu.Lock()
	o.events = append(o.events, event)
	o.mu.Unlock()
	if o.ch != nil {
		o.ch <- event
	}
}

func (o *recordingObserver) snapshot() []Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Event(nil), o.events...)
}

func TestObserverSeesRuntimeAndTaskLifecycle(t *testing.T) {
	observer := &recordingObserver{ch: make(chan Event, 16)}
	rt := NewRuntime(WithObserver(observer))

	if err := rt.Add(Job("job", func(ctx context.Context) error {
		return nil
	})); err != nil {
		t.Fatalf("add job: %v", err)
	}

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	events := observer.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected observer events")
	}

	assertHasEventType(t, events, EventTaskAdded)
	assertHasEventType(t, events, EventRuntimeStarted)
	assertHasEventType(t, events, EventTaskStarted)
	assertHasEventType(t, events, EventTaskFinished)
	assertHasEventType(t, events, EventRuntimeFinished)
	assertHasTaskEvent(t, events, EventTaskFinished, "job")
}

func TestObserverSeesDroppedQueuedJob(t *testing.T) {
	observer := &recordingObserver{ch: make(chan Event, 32)}
	rt := NewRuntime(
		WithObserver(observer),
		WithJobPool(1),
		WithJobQueue(1),
		WithQueueFullPolicy(QueueFullDropOldest),
	)

	started := make(chan struct{})
	release := make(chan struct{})
	serviceStarted := make(chan struct{})

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		close(serviceStarted)
		<-ctx.Done()
		return nil
	})); err != nil {
		t.Fatalf("add service: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-serviceStarted:
	case <-time.After(time.Second):
		t.Fatalf("service did not start")
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
	if err := rt.Add(Job("job-3", func(ctx context.Context) error { return nil })); err != nil {
		t.Fatalf("add job-3: %v", err)
	}

	close(release)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	events := observer.snapshot()
	assertHasTaskEvent(t, events, EventTaskDropped, "job-2")
	assertHasTaskEvent(t, events, EventTaskQueued, "job-3")
	assertHasTaskEvent(t, events, EventTaskFinished, "job-3")
	for _, event := range events {
		if event.Type == EventTaskDropped && event.Task.Name == "job-2" && event.QueueFullPolicy != QueueFullDropOldest {
			t.Fatalf("expected drop-oldest policy, got %v", event.QueueFullPolicy)
		}
	}
}

func TestRuntimeStatsSnapshot(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(1))
	started := make(chan struct{})
	release := make(chan struct{})

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
	if err := rt.Add(Job("job-2", func(ctx context.Context) error { return nil })); err != nil {
		t.Fatalf("add job-2: %v", err)
	}

	stats := rt.Stats()
	if stats.Pending != 2 {
		t.Fatalf("expected pending=2 before start, got %d", stats.Pending)
	}
	if got := stats.Partitions[DefaultJobPartition].QueueLen; got != 0 {
		t.Fatalf("expected empty queue before start, got %d", got)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("job-1 did not start")
	}

	deadline := time.Now().Add(time.Second)
	for {
		stats = rt.Stats()
		if stats.Pending == 0 && stats.Partitions[DefaultJobPartition].QueueLen == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for queued job stats: %+v", stats)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	if err := rt.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}

	stats = rt.Stats()
	if !stats.Finished {
		t.Fatalf("expected finished runtime")
	}
	if stats.Running != 0 {
		t.Fatalf("expected running=0 after wait, got %d", stats.Running)
	}
	if got := stats.Partitions[DefaultJobPartition].QueueLen; got != 0 {
		t.Fatalf("expected empty queue after wait, got %d", got)
	}
}

func assertHasEventType(t *testing.T, events []Event, want EventType) {
	t.Helper()
	for _, event := range events {
		if event.Type == want {
			return
		}
	}
	t.Fatalf("expected event type %q, got %#v", want, events)
}

func assertHasTaskEvent(t *testing.T, events []Event, wantType EventType, wantTask string) {
	t.Helper()
	for _, event := range events {
		if event.Type == wantType && event.Task.Name == wantTask {
			return
		}
	}
	t.Fatalf("expected event %q for task %q, got %#v", wantType, wantTask, events)
}
