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
	Name string
	Kind Kind
	// Partition selects which configured job partition handles this task when
	// Kind is KindJob. Empty means the default job partition.
	Partition string
	// Priority controls queued execution order for pooled jobs. Higher values run
	// first. Jobs with the same priority keep FIFO order.
	Priority int
	Runner   Runner
	mw       []Middleware
}

// Job creates a finite task.
func Job(name string, run Runner) Task {
	return Task{Name: name, Kind: KindJob, Runner: run}
}

// JobWithPriority creates a finite task with the provided queue priority.
//
// Priority only affects pooled jobs. Higher values run first, while jobs with
// equal priority preserve FIFO order.
func JobWithPriority(name string, priority int, run Runner) Task {
	return Task{Name: name, Kind: KindJob, Priority: priority, Runner: run}
}

// WithPriority returns a copy of task with the provided priority.
//
// Priority only affects pooled jobs.
func (t Task) WithPriority(priority int) Task {
	t.Priority = priority
	return t
}

// JobInPartition creates a finite task in the provided job partition.
func JobInPartition(name, partition string, run Runner) Task {
	return Task{Name: name, Kind: KindJob, Partition: partition, Runner: run}
}

// WithPartition returns a copy of task routed to the provided job partition.
//
// Partition only affects jobs.
func (t Task) WithPartition(partition string) Task {
	t.Partition = partition
	return t
}

// WithMiddleware returns a copy of task with middleware applied to its runner.
func (t Task) WithMiddleware(middlewares ...Middleware) Task {
	return WrapTask(t, middlewares...)
}

// Service creates a long-running task.
func Service(name string, run Runner) Task {
	return Task{Name: name, Kind: KindService, Runner: run}
}
