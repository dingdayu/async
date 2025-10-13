// Package async provides lightweight concurrent task orchestration and lifecycle management,
// including task registration, handle and signal processing, and graceful startup and shutdown.
//
// Quick start:
//
//	a := async.NewAsync()
//	// Register tasks and handles...
//	if err := a.Run(ctx); err != nil { /* handle error */ }
//
// See the examples/ directory and README for more samples.
package async
