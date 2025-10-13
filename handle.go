package async

import (
	"context"
)

// Handle represents a task that can be run asynchronously.
type Handle interface {
	Name() string
	Handle(ctx Context)
	OnPreRun()
	OnShutdown(ctx context.Context)
}
