package async

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithFailFastFalseKeepsServiceRunning(t *testing.T) {
	rt := NewRuntime(WithFailFast(false))
	started := make(chan struct{})
	stopped := make(chan struct{})
	boom := errors.New("boom")

	if err := rt.Add(Service("svc", func(ctx context.Context) error {
		close(started)
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

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("service did not start")
	}

	select {
	case <-stopped:
		t.Fatalf("service stopped while fail-fast disabled")
	case <-time.After(150 * time.Millisecond):
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := rt.Shutdown(shutdownCtx)
	if err == nil {
		t.Fatalf("expected shutdown to return failure")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom")
	}
}

func TestWaitBeforeStart(t *testing.T) {
	rt := NewRuntime()
	err := rt.Wait()
	if !errors.Is(err, ErrRuntimeNotStarted) {
		t.Fatalf("expected ErrRuntimeNotStarted, got %v", err)
	}
}
