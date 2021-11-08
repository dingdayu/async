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
	c.Context.Done()
	_ = c.async.UnRegister(c.handle, ExitSignal{})
}

func (c *Context) Done() <-chan struct{} {
	return c.Context.Done()
}
