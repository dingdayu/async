package async

import "context"

// Task is a convenience struct implementation of the Handle interface.
// It allows users to provide callbacks instead of implementing the full interface.
type Task struct {
	NameStr  string
	Run      func(Context)
	PreRun   func()
	Shutdown func(context.Context)
}

// Ensure Task implements Handle
var _ Handle = (*Task)(nil)

// Name returns the task name
func (t *Task) Name() string {
	return t.NameStr
}

// Handle calls the user-supplied Run callback if present
func (t *Task) Handle(ctx Context) {
	if t == nil {
		return
	}
	if t.Run != nil {
		t.Run(ctx)
	}
}

// OnPreRun calls PreRun if set
func (t *Task) OnPreRun() {
	if t == nil {
		return
	}
	if t.PreRun != nil {
		t.PreRun()
	}
}

// OnShutdown calls Shutdown if set
func (t *Task) OnShutdown(ctx context.Context) {
	if t == nil {
		return
	}
	if t.Shutdown != nil {
		t.Shutdown(ctx)
	}
}

// TaskOption configures a Task during construction
type TaskOption func(*Task)

// WithTaskPreRun sets a PreRun callback
func WithTaskPreRun(fn func()) TaskOption {
	return func(t *Task) { t.PreRun = fn }
}

// WithTaskShutdown sets a Shutdown callback
func WithTaskShutdown(fn func(context.Context)) TaskOption {
	return func(t *Task) { t.Shutdown = fn }
}

// NewTask constructs a Task with a name, run callback and optional options.
func NewTask(name string, run func(Context), opts ...TaskOption) *Task {
	t := &Task{NameStr: name, Run: run}
	for _, o := range opts {
		o(t)
	}
	return t
}
