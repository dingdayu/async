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
	"os"
	"sync"
)

// Async async
type Async struct {
	ctx context.Context
	wg  *sync.WaitGroup

	mu sync.RWMutex

	handlesSort []string
	handles     map[string]HandleArg
}

// HandleArg handle arg: context, cancel
type HandleArg struct {
	call   Handle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAsync new async
func NewAsync(ctx context.Context, ch <-chan os.Signal) *Async {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	var asy = Async{ctx: ctx, wg: &wg, handles: map[string]HandleArg{}}

	go func() {
		// Wait for exit signal.
		s := <-ch

		asy.mu.RLock()
		for _, handle := range asy.handles {
			go handle.call.OnShutdown(s)
		}
		asy.mu.RUnlock()

		// Notify context exit.
		cancel()
	}()

	return &asy
}

// Register register async handle
func (a *Async) Register(call Handle) error {
	defer func() {
		if err := recover(); err != nil {
			a.wg.Done()
			err = errors.New("register error")
		}
	}()
	a.wg.Add(1)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.handlesSort = append(a.handlesSort, call.Name())

	handleArg := HandleArg{call: call}
	handleArg.ctx, handleArg.cancel = context.WithCancel(a.ctx)
	a.handles[call.Name()] = handleArg

	// pre
	a.handles[call.Name()].call.OnPreRun()

	// run
	go a.handles[call.Name()].call.Handle(handleArg.ctx, a.wg)

	return nil
}

// UnRegister unregister async handle
func (a *Async) UnRegister(handle Handle) error {
	a.wg.Add(1)

	if call, ok := a.handles[handle.Name()]; ok {
		a.mu.Lock()

		// cancel & shutdown
		call.cancel()
		call.call.OnShutdown(UnRegisterSignal{})

		a.mu.Unlock()
		return nil
	}

	return errors.New("not fund handle")
}

// Wait async wait
func (a *Async) Wait() {
	a.wg.Wait()
}
