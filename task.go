package async

import "context"

// Runner executes a task attempt.
//
// Returning nil means the attempt completed successfully. Returning a non-nil
// error marks the task as failed.
type Runner func(context.Context) error

// Kind describes the intended lifecycle of a task.
type Kind uint8

const (
	// KindJob represents finite work that is expected to complete.
	KindJob Kind = iota
	// KindService represents long-running work that is expected to stay active
	// until shutdown.
	KindService
)

// Task describes a named unit of work managed by Runtime.
type Task struct {
	Name   string
	Kind   Kind
	Runner Runner
}

// Job creates a finite task.
func Job(name string, run Runner) Task {
	return Task{Name: name, Kind: KindJob, Runner: run}
}

// Service creates a long-running task.
func Service(name string, run Runner) Task {
	return Task{Name: name, Kind: KindService, Runner: run}
}
