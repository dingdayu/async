package async

import (
	"context"
	"os"
	"sync"
)

// Handle async Handle
type Handle func(ctx context.Context, wg *sync.WaitGroup)

// Shutdown async shutdown func
type Shutdown func(s os.Signal)

// Async async
type Async struct {
	ctx context.Context
	wg  *sync.WaitGroup

	mu         sync.Mutex
	onShutdown []Shutdown
}

// NewAsync new async
func NewAsync(ctx context.Context, ch <-chan os.Signal) *Async {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	var asy = Async{ctx: ctx, wg: &wg}

	go func() {
		s := <-ch
		for _, f := range asy.onShutdown {
			go f(s)
		}

		cancel()
	}()

	return &asy
}

// RegisterOnShutdown register func to shutdown before
func (a *Async) RegisterOnShutdown(f Shutdown) {
	a.mu.Lock()
	a.onShutdown = append(a.onShutdown, f)
	a.mu.Unlock()
}

// Register register async handle
func (a *Async) Register(f Handle) {
	a.wg.Add(1)

	go f(a.ctx, a.wg)
}

// Wait async wait
func (a *Async) Wait() {
	a.wg.Wait()
}
