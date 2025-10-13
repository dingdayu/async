/*
   Copyright [2020] dingdayu <https://github.com/dingdayu>

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

/*
This is Safe asynchronous tasks by Go.
*/
package async

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Async async
type Async struct {
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	startOnce sync.Once
	stopOnce  sync.Once
	started   bool

	wg *sync.WaitGroup

	handlesSort []string
	handles     map[Handle]*HandleArg
	onShutdown  []func(context.Context)

	logger *slog.Logger
	// per-hook timeout; zero means no timeout
	hookTimeout time.Duration
}

// package-level exported errors
var (
	ErrHandleAlreadyRegistered = errors.New("handle already registered")
	ErrHandleNotFound          = errors.New("handle not found")
)

// Option configures Async created by NewAsync
type Option func(*Async)

// WithLogger supplies a slog.Logger to Async (overrides default slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(a *Async) {
		if l != nil {
			a.logger = l
		}
	}
}

// WithHookTimeout sets a per-hook timeout for shutdown hooks. If zero, hooks run without timeout.
func WithHookTimeout(d time.Duration) Option {
	return func(a *Async) { a.hookTimeout = d }
}

// (signal injection removed)

// HandleArg handle arg: context, cancel
type HandleArg struct {
	call    Handle
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}

// NewAsync creates a new Async instance. Context is provided later via Run/Start.
func NewAsync(opts ...Option) *Async {
	var wg sync.WaitGroup
	asy := &Async{
		wg:      &wg,
		handles: map[Handle]*HandleArg{},
		logger:  newDefaultLogger(),
	}
	for _, opt := range opts {
		opt(asy)
	}
	asy.logger = ensureLogger(asy.logger)
	return asy
}

// Run starts the async manager and blocks until all handles exit.
func (a *Async) Run(ctx context.Context) error {
	stop, err := a.Start(ctx)
	if err != nil {
		return err
	}
	defer stop()

	a.Wait()
	return nil
}

// Start launches the async manager without waiting for handles to exit.
func (a *Async) Start(ctx context.Context) (func(), error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}

	a.mu.RLock()
	alreadyStarted := a.started
	a.mu.RUnlock()
	if alreadyStarted {
		return func() { a.Stop() }, nil
	}

	a.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)

		a.mu.Lock()
		a.ctx = runCtx
		a.cancel = cancel
		a.started = true
		a.mu.Unlock()

		a.startPendingHandles()

		go a.watchSignals(runCtx)
	})

	a.mu.RLock()
	alreadyStarted = a.started
	a.mu.RUnlock()
	if !alreadyStarted {
		return nil, errors.New("async failed to start")
	}

	return func() { a.Stop() }, nil
}

// Stop triggers shutdown and is safe to call multiple times.
func (a *Async) Stop() {
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	if !started {
		return
	}
	a.shutdown()
}

func (a *Async) watchSignals(ctx context.Context) {
	sigCtx, sigCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer sigCancel()

	<-sigCtx.Done()
	a.shutdown()
}

func (a *Async) startPendingHandles() {
	a.mu.Lock()
	if a.ctx == nil {
		a.mu.Unlock()
		return
	}

	pending := make([]*HandleArg, 0)
	for _, arg := range a.handles {
		if arg.started {
			continue
		}
		arg.ctx, arg.cancel = context.WithCancel(a.ctx)
		arg.started = true
		pending = append(pending, arg)
	}
	a.mu.Unlock()

	for _, arg := range pending {
		go arg.call.Handle(Context{arg.ctx, a, arg.call})
	}
}

func (a *Async) shutdown() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		if !a.started {
			a.mu.Unlock()
			return
		}

		cancel := a.cancel
		handles := make([]Handle, 0, len(a.handles))
		for h := range a.handles {
			handles = append(handles, h)
		}
		shutdowns := make([]func(context.Context), len(a.onShutdown))
		copy(shutdowns, a.onShutdown)
		timeout := a.hookTimeout
		logger := a.logger
		a.mu.Unlock()

		if cancel != nil {
			cancel()
		}

		logger.Info("received shutdown")

		var hooksWg sync.WaitGroup
		hooksWg.Add(len(shutdowns))
		for _, fn := range shutdowns {
			hook := fn
			go func() {
				defer hooksWg.Done()
				if hook == nil {
					return
				}
				if timeout <= 0 {
					hook(context.Background())
					return
				}
				hookCtx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				hook(hookCtx)
				if hookCtx.Err() == context.DeadlineExceeded {
					logger.Warn("shutdown hook timeout", slog.String("timeout", timeout.String()))
				}
			}()
		}
		hooksWg.Wait()

		var unregWg sync.WaitGroup
		unregWg.Add(len(handles))
		for _, handle := range handles {
			h := handle
			go func() {
				defer unregWg.Done()
				_ = a.UnRegister(h)
			}()
		}
		unregWg.Wait()
	})
}

// Register register async handle
//
// Example usage with Task convenience helper:
//
//	t := NewTask("worker", func(ctx Context) {
//	    defer ctx.Exit()
//	    // work loop
//	})
//	_ = a.Register(t)
func (a *Async) Register(call Handle) error {
	defer func() {
		if err := recover(); err != nil {
			// Only call Done if Add was called and the handle was registered
			a.mu.Lock()
			if _, ok := a.handles[call]; ok {
				a.wg.Done()
				delete(a.handles, call)
			}
			a.mu.Unlock()
			// err = errors.New("register error") // Not used
		}
	}()

	handleArg := &HandleArg{call: call}

	a.mu.Lock()
	// Prevent double registration (same Handle instance)
	if _, exists := a.handles[call]; exists {
		a.mu.Unlock()
		return ErrHandleAlreadyRegistered
	}

	// Detect name collision with existing handles (different instance but same Name())
	name := call.Name()
	for h := range a.handles {
		if h.Name() == name {
			// warn but allow registration (keep existing logic)
			a.logger.Warn("registering handle with duplicate Name", slog.String("name", name))
			break
		}
	}

	// Record in structures
	a.handles[call] = handleArg
	a.handlesSort = append(a.handlesSort, name)

	a.wg.Add(1) // increment after successful registration

	// Determine whether to start immediately (if async already running)
	var shouldStart bool
	if a.started {
		handleArg.ctx, handleArg.cancel = context.WithCancel(a.ctx)
		handleArg.started = true
		shouldStart = true
	}

	// pre (call under lock so concurrent UnRegister can't remove midway)
	call.OnPreRun()
	a.mu.Unlock()

	if shouldStart {
		go call.Handle(Context{handleArg.ctx, a, call})
	}

	a.logger.Info("registered handle", slog.String("name", name))
	return nil
}

// UnRegister unregister async handle
func (a *Async) UnRegister(handle Handle) error {
	a.mu.Lock()
	handleArg, ok := a.handles[handle]
	if !ok {
		a.mu.Unlock()
		return ErrHandleNotFound
	}

	// remove from map and handlesSort
	delete(a.handles, handle)
	// remove first matching name from handlesSort
	name := handle.Name()
	for i, v := range a.handlesSort {
		if v == name {
			a.handlesSort = append(a.handlesSort[:i], a.handlesSort[i+1:]...)
			break
		}
	}

	// cancel while unlocked to avoid potential deadlocks, but keep call variable
	a.mu.Unlock()

	// cancel & shutdown - protect OnShutdown from panics
	if handleArg.cancel != nil {
		handleArg.cancel()
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("panic in OnShutdown", slog.String("name", name), slog.Any("panic", r))
			}
		}()
		// call OnShutdown with a background context
		handleArg.call.OnShutdown(context.Background())
	}()

	a.logger.Info("unregistered handle", slog.String("name", name))
	a.wg.Done()
	return nil
}

// Wait async wait
func (a *Async) Wait() {
	a.wg.Wait()
}

// RegisterOnShutdown registers a function to be called when the async receives a shutdown signal.
func (a *Async) RegisterOnShutdown(fn func(context.Context)) {
	if fn == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onShutdown = append(a.onShutdown, fn)
}
