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
