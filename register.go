package async

import "context"

// DefaultAsync is a global async instance for convenience, similar to prometheus.DefaultRegisterer.
var DefaultAsync = NewAsync()

// Start starts the DefaultAsync instance with the provided context.
func Start(ctx context.Context) (func(), error) {
	return DefaultAsync.Start(ctx)
}

// Run starts DefaultAsync and blocks until all handles exit.
func Run(ctx context.Context) error {
	return DefaultAsync.Run(ctx)
}

// Register registers a handle to DefaultAsync.
func Register(h Handle) error {
	return DefaultAsync.Register(h)
}

// Wait blocks until all handles in DefaultAsync have exited.
func Wait() {
	DefaultAsync.Wait()
}
