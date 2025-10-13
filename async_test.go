package async

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestNewAsync(t *testing.T) {
	// Simple construction test: NewAsync should return non-nil
	if got := NewAsync(); got == nil {
		t.Errorf("NewAsync() returned nil")
	}
}

func TestAsync_RegisterOnShutdown(t *testing.T) {
	asy := NewAsync()
	asy.RegisterOnShutdown(func(ctx context.Context) {
		// no-op
	})
	if len(asy.onShutdown) != 1 {
		t.Error("RegisterOnShutdown() Shutdown register error")
	}
}

func TestAsync_StopBeforeStart(t *testing.T) {
	asy := NewAsync()
	asy.Stop() // should be a no-op before Start

	stop, err := asy.Start(context.Background())
	if err != nil {
		t.Fatalf("Start returned error after Stop: %v", err)
	}
	stop()
}

func TestAsync_RunBlocksUntilShutdown(t *testing.T) {
	asy := NewAsync()
	handle := &runHandle{started: make(chan struct{})}
	if err := asy.Register(handle); err != nil {
		t.Fatalf("register handle: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-handle.started
		cancel()
	}()

	if err := asy.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if atomic.LoadInt32(&handle.shutdownCalled) != 1 {
		t.Fatalf("expected shutdown to be called once")
	}
}

type runHandle struct {
	started        chan struct{}
	shutdownCalled int32
}

func (h *runHandle) Name() string { return "run-handle" }

func (h *runHandle) Handle(ctx Context) {
	defer ctx.Exit()
	close(h.started)
	<-ctx.Done()
}

func (h *runHandle) OnPreRun() {}

func (h *runHandle) OnShutdown(ctx context.Context) {
	atomic.AddInt32(&h.shutdownCalled, 1)
}
