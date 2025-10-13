package async

import (
	"context"
)

// DefaultAsync is a global async instance for convenience, similar to prometheus.DefaultRegisterer.
var DefaultAsync *Async

func init() {
	DefaultAsync = NewAsync(context.Background())
}

// Register registers a handle to DefaultAsync.
func Register(h Handle) error {
	return DefaultAsync.Register(h)
}

// Wait blocks until all handles in DefaultAsync have exited.
func Wait() {
	DefaultAsync.Wait()
}
