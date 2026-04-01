package async

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"runtime/debug"
	"sync"
)

var (
	// ErrNilContext is returned when a nil context is passed to a lifecycle call.
	ErrNilContext = errors.New("context must not be nil")
	// ErrTaskNameRequired is returned when a task is added without a name.
	ErrTaskNameRequired = errors.New("task name is required")
	// ErrNilRunner is returned when a task has no runner function.
	ErrNilRunner = errors.New("task runner must not be nil")
	// ErrTaskAlreadyExists is returned when a task with the same name already exists.
	ErrTaskAlreadyExists = errors.New("task already exists")
	// ErrRuntimeNotStarted is returned when Wait is called before Start or Run.
	ErrRuntimeNotStarted = errors.New("runtime not started")
	// ErrRuntimeAlreadyEnded is returned when work is added after the runtime finishes.
	ErrRuntimeAlreadyEnded = errors.New("runtime already ended")
	// ErrRuntimeShuttingDown is returned when work is added during shutdown.
	ErrRuntimeShuttingDown = errors.New("runtime is shutting down")
	// ErrUnexpectedServiceExit is returned when a service exits cleanly before shutdown.
	ErrUnexpectedServiceExit = errors.New("service returned before shutdown")
	// ErrJobQueueFull is returned by TryAdd when the job queue is saturated.
	ErrJobQueueFull = errors.New("job queue is full")
)

// Option configures a Runtime.
type Option func(*Runtime)

// WithFailFast controls whether the first task failure cancels the rest of the runtime.
func WithFailFast(enabled bool) Option {
	return func(r *Runtime) {
		r.failFast = enabled
	}
}

// WithJobPool configures how many worker goroutines execute queued jobs.
//
// Set workers to 0 to disable pooling and run jobs in dedicated goroutines.
func WithJobPool(workers int) Option {
	if workers < 0 {
		workers = 0
	}
	return func(r *Runtime) {
		r.jobWorkers = workers
	}
}

// WithJobQueue configures the maximum number of queued jobs waiting for a pool
// worker.
//
// Set capacity to 0 for an unbounded queue.
func WithJobQueue(capacity int) Option {
	if capacity < 0 {
		capacity = 0
	}
	return func(r *Runtime) {
		r.jobQueueCap = capacity
	}
}

// Runtime supervises named tasks and coordinates startup, failure handling, and shutdown.
type Runtime struct {
	mu sync.Mutex

	failFast bool

	started  bool
	closed   bool
	finished bool

	ctx    context.Context
	cancel context.CancelFunc

	waitCh    chan struct{}
	closeOnce sync.Once

	pending map[string]Task
	running map[string]Kind

	runningCount int
	firstErr     error

	jobWorkers  int
	jobQueue    []Task
	jobQueueCap int
	jobCond     *sync.Cond
}

// TaskError reports a task-level failure observed by Runtime.
type TaskError struct {
	Task  string
	Kind  Kind
	Cause error
}

func (e *TaskError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("task %q (%s) failed: %v", e.Task, e.Kind.String(), e.Cause)
}

func (e *TaskError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (k Kind) String() string {
	if k == KindService {
		return "service"
	}
	return "job"
}

// NewRuntime creates a new Runtime with the provided options.
func NewRuntime(opts ...Option) *Runtime {
	r := &Runtime{
		failFast:   true,
		waitCh:     make(chan struct{}),
		pending:    make(map[string]Task),
		running:    make(map[string]Kind),
		jobWorkers: goruntime.GOMAXPROCS(0),
	}
	r.jobCond = sync.NewCond(&r.mu)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// NewAsync is kept as a compatibility alias for NewRuntime.
func NewAsync(opts ...Option) *Runtime {
	return NewRuntime(opts...)
}

// Add registers a task with the runtime.
//
// Tasks may be added before startup or while the runtime is running, but not
// after shutdown has begun or after the runtime has finished.
func (r *Runtime) Add(task Task) error {
	return r.add(task, true)
}

// TryAdd registers a task without blocking on pooled job queue backpressure.
//
// For pooled jobs, TryAdd returns ErrJobQueueFull when the queue is at
// capacity. Non-pooled tasks follow the same semantics as Add.
func (r *Runtime) TryAdd(task Task) error {
	return r.add(task, false)
}

func (r *Runtime) add(task Task, blockOnQueue bool) error {
	if task.Name == "" {
		return ErrTaskNameRequired
	}
	if task.Runner == nil {
		return ErrNilRunner
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finished {
		return ErrRuntimeAlreadyEnded
	}
	if r.closed {
		return ErrRuntimeShuttingDown
	}
	if _, exists := r.pending[task.Name]; exists {
		return ErrTaskAlreadyExists
	}
	if _, exists := r.running[task.Name]; exists {
		return ErrTaskAlreadyExists
	}

	if !r.started {
		r.pending[task.Name] = task
		return nil
	}

	return r.launchLocked(task, blockOnQueue)
}

// Start launches all pending tasks and begins supervising the runtime.
func (r *Runtime) Start(parent context.Context) error {
	if parent == nil {
		return ErrNilContext
	}

	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return ErrRuntimeAlreadyEnded
	}
	if r.started {
		r.mu.Unlock()
		return nil
	}

	r.started = true
	r.ctx, r.cancel = context.WithCancel(parent)
	r.startJobWorkersLocked()

	for name, task := range r.pending {
		delete(r.pending, name)
		if err := r.launchLocked(task, true); err != nil {
			r.pending[name] = task
			break
		}
	}

	if r.runningCount == 0 {
		r.finished = true
	}
	finished := r.finished
	r.mu.Unlock()

	if finished {
		r.closeWait()
	}
	return nil
}

// Run starts the runtime and waits for it to finish.
func (r *Runtime) Run(ctx context.Context) error {
	if err := r.Start(ctx); err != nil {
		return err
	}
	return r.Wait()
}

// Shutdown requests runtime shutdown and waits until running tasks return or the
// shutdown context expires.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	r.mu.Lock()
	if r.finished {
		err := r.firstErr
		r.mu.Unlock()
		return err
	}
	if !r.started {
		r.closed = true
		r.finished = true
		err := r.firstErr
		r.mu.Unlock()
		r.closeWait()
		return err
	}

	var cancel context.CancelFunc
	if !r.closed {
		r.closed = true
		cancel = r.cancel
		r.jobCond.Broadcast()
	}
	if r.runningCount == 0 {
		r.finished = true
	}
	finished := r.finished
	waitCh := r.waitCh
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if finished {
		r.closeWait()
		r.mu.Lock()
		err := r.firstErr
		r.mu.Unlock()
		return err
	}

	select {
	case <-waitCh:
		r.mu.Lock()
		err := r.firstErr
		r.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait blocks until the runtime reaches its terminal state and returns the
// first task failure, if any.
func (r *Runtime) Wait() error {
	r.mu.Lock()
	if !r.started && !r.finished {
		r.mu.Unlock()
		return ErrRuntimeNotStarted
	}
	waitCh := r.waitCh
	r.mu.Unlock()

	<-waitCh

	r.mu.Lock()
	err := r.firstErr
	r.mu.Unlock()
	return err
}

func (r *Runtime) startJobWorkersLocked() {
	if r.jobWorkers <= 0 || r.ctx == nil {
		return
	}
	for i := 0; i < r.jobWorkers; i++ {
		go r.jobWorker()
	}
}

func (r *Runtime) launchLocked(task Task, blockOnQueue bool) error {
	if task.Kind == KindJob && r.jobWorkers > 0 {
		if err := r.enqueueJobLocked(task, blockOnQueue); err != nil {
			return err
		}
		r.runningCount++
		r.running[task.Name] = task.Kind
		return nil
	}

	r.runningCount++
	r.running[task.Name] = task.Kind
	ctx := r.ctx
	go r.execute(ctx, task)
	return nil
}

func (r *Runtime) enqueueJobLocked(task Task, blockOnQueue bool) error {
	for {
		if r.finished {
			return ErrRuntimeAlreadyEnded
		}
		if r.closed {
			return ErrRuntimeShuttingDown
		}
		if r.jobQueueCap <= 0 || len(r.jobQueue) < r.jobQueueCap {
			r.jobQueue = append(r.jobQueue, task)
			r.jobCond.Signal()
			return nil
		}
		if !blockOnQueue {
			return ErrJobQueueFull
		}
		r.jobCond.Wait()
	}
}

func (r *Runtime) jobWorker() {
	for {
		r.mu.Lock()
		for len(r.jobQueue) == 0 && !r.finished {
			r.jobCond.Wait()
		}
		if len(r.jobQueue) == 0 && r.finished {
			r.mu.Unlock()
			return
		}
		task := r.jobQueue[0]
		r.jobQueue[0] = Task{}
		r.jobQueue = r.jobQueue[1:]
		r.jobCond.Broadcast()
		ctx := r.ctx
		r.mu.Unlock()

		r.execute(ctx, task)
	}
}

func (r *Runtime) execute(ctx context.Context, task Task) {
	err := runTask(ctx, task)

	r.mu.Lock()
	delete(r.running, task.Name)
	r.runningCount--

	if err != nil && r.firstErr == nil {
		r.firstErr = &TaskError{Task: task.Name, Kind: task.Kind, Cause: err}
	}

	var cancel context.CancelFunc
	if err != nil && r.failFast && !r.closed {
		r.closed = true
		cancel = r.cancel
		r.jobCond.Broadcast()
	}

	if r.runningCount == 0 {
		r.finished = true
	}
	finished := r.finished
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if finished {
		r.closeWait()
	}
}

func (r *Runtime) closeWait() {
	r.closeOnce.Do(func() {
		close(r.waitCh)
		r.mu.Lock()
		r.jobCond.Broadcast()
		r.mu.Unlock()
	})
}

func runTask(ctx context.Context, task Task) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic: %v\n%s", rec, string(debug.Stack()))
		}
	}()

	err = task.Runner(ctx)
	if err == nil && task.Kind == KindService && ctx.Err() == nil {
		return ErrUnexpectedServiceExit
	}
	return err
}
