package async

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
)

// test handle implementation
type testHandle struct {
	name           string
	shutdownCalled int32
}

func (t *testHandle) Name() string                   { return t.name }
func (t *testHandle) Handle(ctx Context)             { <-ctx.Done() }
func (t *testHandle) OnPreRun()                      {}
func (t *testHandle) OnShutdown(ctx context.Context) { atomic.AddInt32(&t.shutdownCalled, 1) }

// test logger capturing logs to buffer
type bufHandler struct{ buf *bytes.Buffer }

func (h bufHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h bufHandler) Handle(ctx context.Context, r slog.Record) error {
	fmt.Fprintf(h.buf, "%s %v\n", r.Level, r.Message)
	return nil
}
func (h bufHandler) WithAttrs(a []slog.Attr) slog.Handler { return h }
func (h bufHandler) WithGroup(name string) slog.Handler   { return h }

// Note: WithUseContextSignal removed in v4; use context cancellation or signals to trigger shutdown.

func TestRegister_DuplicateNameWarn(t *testing.T) {
	bh := &bufHandler{buf: &bytes.Buffer{}}
	logger := slog.New(bh)
	asy := NewAsync(WithLogger(logger))

	h1 := &testHandle{name: "dup"}
	h2 := &testHandle{name: "dup"}
	if err := asy.Register(h1); err != nil {
		t.Fatalf("register h1 failed: %v", err)
	}
	if err := asy.Register(h2); err != nil {
		t.Fatalf("register h2 failed: %v", err)
	}

	// ensure logs contain warning message
	out := bh.buf.String()
	if out == "" {
		t.Fatalf("expected logs, got none")
	}
	if !bytes.Contains([]byte(out), []byte("registering handle with duplicate Name")) {
		t.Fatalf("expected duplicate name warning in logs, got: %s", out)
	}

	stop, err := asy.Start(context.Background())
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	stop()
}

func TestNewAsync_ChNil_NotifyContext(t *testing.T) {
	// ch == nil should cause NewAsync to use signal.NotifyContext internally.
	ctx, cancel := context.WithCancel(context.Background())
	bh := &bufHandler{buf: &bytes.Buffer{}}
	logger := slog.New(bh)

	asy := NewAsync(WithLogger(logger))

	th := &testHandle{name: "t-ctx"}
	if err := asy.Register(th); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	stop, err := asy.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// cancel context triggers internal signal context
	cancel()

	asy.Wait()
	stop()

	if atomic.LoadInt32(&th.shutdownCalled) == 0 {
		t.Fatalf("expected handle OnShutdown to be called when ch==nil and ctx canceled")
	}
}

func TestShutdownHooksConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bh := &bufHandler{buf: &bytes.Buffer{}}
	logger := slog.New(bh)

	asy := NewAsync(WithLogger(logger))

	order := make([]int32, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	asy.RegisterOnShutdown(func(ctx context.Context) {
		atomic.AddInt32(&order[0], 1)
		wg.Done()
	})
	asy.RegisterOnShutdown(func(ctx context.Context) {
		atomic.AddInt32(&order[1], 1)
		wg.Done()
	})
	asy.RegisterOnShutdown(func(ctx context.Context) {
		atomic.AddInt32(&order[2], 1)
		wg.Done()
	})

	stop, err := asy.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// cancel context to trigger hooks
	cancel()
	wg.Wait()
	stop()
}
