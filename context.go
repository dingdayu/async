package async

import (
	"context"
)

// Context is the context passed to Handle's Handle method.
type Context struct {
	context.Context

	async  *Async
	handle Handle
}

// Exit signals that the handle has completed its work and is exiting.
func (c *Context) Exit() {
	// Unregister the handle; no signal to pass.
	_ = c.async.UnRegister(c.handle)
}

// Done returns a channel that's closed when work done on behalf of this context should be canceled.
func (c *Context) Done() <-chan struct{} {
	return c.Context.Done()
}
