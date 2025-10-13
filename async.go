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
	ctx context.Context
	wg  *sync.WaitGroup

	mu sync.RWMutex

	handlesSort []string
	handles     map[Handle]HandleArg
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
	call   Handle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAsync new async
func NewAsync(ctx context.Context, opts ...Option) *Async {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	asy := Async{ctx: ctx, wg: &wg, handles: map[Handle]HandleArg{}, logger: newDefaultLogger()}
	// apply options
	for _, opt := range opts {
		opt(&asy)
	}
	asy.logger = ensureLogger(asy.logger)

	go func() {
		// always create a signal-notify context to handle OS signals
		sigCtx, sigCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer sigCancel()

		// wait for signal context cancellation

		<-sigCtx.Done()

		// Make a snapshot of handles and shutdown hooks
		asy.mu.RLock()
		handles := make([]Handle, 0, len(asy.handles))
		for h := range asy.handles {
			handles = append(handles, h)
		}
		shutdowns := make([]func(context.Context), len(asy.onShutdown))
		copy(shutdowns, asy.onShutdown)
		asy.mu.RUnlock()

		asy.logger.Info("received shutdown")

		// run registered shutdown hooks concurrently and wait for them to finish
		var hooksWg sync.WaitGroup
		hooksWg.Add(len(shutdowns))
		for _, fn := range shutdowns {
			f := fn
			go func() {
				defer hooksWg.Done()
				// enforce per-hook timeout if configured
				timeout := asy.hookTimeout
				if timeout <= 0 {
					f(context.Background())
					return
				}
				hookCtxTmp, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				f(hookCtxTmp)
				if hookCtxTmp.Err() == context.DeadlineExceeded {
					asy.logger.Warn("shutdown hook timeout", slog.String("timeout", timeout.String()))
				}
			}()
		}
		hooksWg.Wait()

		// concurrently unregister handles and wait
		var unregWg sync.WaitGroup
		unregWg.Add(len(handles))
		for _, handle := range handles {
			h := handle
			go func() {
				_ = asy.UnRegister(h)
				unregWg.Done()
			}()
		}
		unregWg.Wait()

		// Notify context exit.
		cancel()
	}()

	return &asy
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

	// Prepare handleArg before acquiring lock to minimize locked time
	handleArg := HandleArg{call: call}
	handleArg.ctx, handleArg.cancel = context.WithCancel(a.ctx)

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

	// pre (call under lock so concurrent UnRegister can't remove midway)
	call.OnPreRun()
	a.mu.Unlock()

	// run with a copy of ctx and handle to avoid races on map key
	go call.Handle(Context{handleArg.ctx, a, call})

	a.logger.Info("registered handle", slog.String("name", name))
	return nil
}

// UnRegister unregister async handle
func (a *Async) UnRegister(handle Handle) error {
	a.mu.Lock()
	call, ok := a.handles[handle]
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
	call.cancel()
	func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("panic in OnShutdown", slog.String("name", name), slog.Any("panic", r))
			}
		}()
		// call OnShutdown with a background context
		call.call.OnShutdown(context.Background())
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
