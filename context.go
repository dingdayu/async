package async

import (
	"context"
)

type Context struct {
	context.Context

	async  *Async
	handle Handle
}

func (c *Context) Exit() {
	// Unregister the handle; no signal to pass.
	_ = c.async.UnRegister(c.handle)
}

func (c *Context) Done() <-chan struct{} {
	return c.Context.Done()
}
