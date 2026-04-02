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
	// ErrUnknownJobPartition is returned when a job targets an unconfigured partition.
	ErrUnknownJobPartition = errors.New("unknown job partition")
)

const (
	// DefaultJobPartition is the built-in partition used by jobs that do not
	// explicitly select one.
	DefaultJobPartition = "default"
)

// Option configures a Runtime.
type Option func(*Runtime)

// QueueFullPolicy controls how pooled job submission behaves when a bounded
// queue reaches capacity.
type QueueFullPolicy uint8

const (
	// QueueFullBlock waits for queue capacity. TryAdd still returns
	// ErrJobQueueFull because it must remain non-blocking.
	QueueFullBlock QueueFullPolicy = iota
	// QueueFullReject rejects new jobs with ErrJobQueueFull.
	QueueFullReject
	// QueueFullDropOldest drops the oldest queued job to make room.
	QueueFullDropOldest
	// QueueFullDropNewest drops the newest queued job to make room.
	QueueFullDropNewest
)

// JobPartitionConfig configures a named job partition.
type JobPartitionConfig struct {
	Workers         int
	QueueCap        int
	QueueFullPolicy QueueFullPolicy
}

type jobPartitionConfig struct {
	workers         int
	queueCap        int
	queueFullPolicy QueueFullPolicy
}

type jobPartition struct {
	name string
	jobPartitionConfig
	queue      []queuedJob
	enqueueSeq uint64
	cond       *sync.Cond
}

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
		partition := r.ensureJobPartition(DefaultJobPartition)
		partition.workers = workers
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
		partition := r.ensureJobPartition(DefaultJobPartition)
		partition.queueCap = capacity
	}
}

// WithQueueFullPolicy configures how pooled jobs behave when a bounded queue is
// full.
//
// QueueFullBlock is the default and preserves legacy behavior.
func WithQueueFullPolicy(policy QueueFullPolicy) Option {
	if policy > QueueFullDropNewest {
		policy = QueueFullBlock
	}
	return func(r *Runtime) {
		partition := r.ensureJobPartition(DefaultJobPartition)
		partition.queueFullPolicy = policy
	}
}

// WithJobPartition registers or updates a named job partition.
func WithJobPartition(name string, cfg JobPartitionConfig) Option {
	if cfg.Workers < 0 {
		cfg.Workers = 0
	}
	if cfg.QueueCap < 0 {
		cfg.QueueCap = 0
	}
	if cfg.QueueFullPolicy > QueueFullDropNewest {
		cfg.QueueFullPolicy = QueueFullBlock
	}

	return func(r *Runtime) {
		partition := r.ensureJobPartition(normalizeJobPartition(name))
		partition.workers = cfg.Workers
		partition.queueCap = cfg.QueueCap
		partition.queueFullPolicy = cfg.QueueFullPolicy
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

	jobPartitions map[string]*jobPartition
}

type queuedJob struct {
	task Task
	seq  uint64
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
	defaultPartition := &jobPartition{
		name: DefaultJobPartition,
		jobPartitionConfig: jobPartitionConfig{
			workers:         goruntime.GOMAXPROCS(0),
			queueFullPolicy: QueueFullBlock,
		},
	}

	r := &Runtime{
		failFast:      true,
		waitCh:        make(chan struct{}),
		pending:       make(map[string]Task),
		running:       make(map[string]Kind),
		jobPartitions: map[string]*jobPartition{DefaultJobPartition: defaultPartition},
	}
	defaultPartition.cond = sync.NewCond(&r.mu)
	for _, opt := range opts {
		opt(r)
	}
	for _, partition := range r.jobPartitions {
		if partition.cond == nil {
			partition.cond = sync.NewCond(&r.mu)
		}
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
	if err := r.validateJobPartitionLocked(task); err != nil {
		return err
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
		r.broadcastJobPartitionsLocked()
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
	if r.ctx == nil {
		return
	}
	for _, partition := range r.jobPartitions {
		if partition.workers <= 0 {
			continue
		}
		for i := 0; i < partition.workers; i++ {
			go r.jobWorker(partition)
		}
	}
}

func (r *Runtime) launchLocked(task Task, blockOnQueue bool) error {
	if task.Kind == KindJob {
		partition, err := r.jobPartitionForTaskLocked(task)
		if err != nil {
			return err
		}
		if partition.workers > 0 {
			if err := r.enqueueJobLocked(partition, task, blockOnQueue); err != nil {
				return err
			}
			r.runningCount++
			r.running[task.Name] = task.Kind
			return nil
		}

		ctx := r.ctx
		r.runningCount++
		r.running[task.Name] = task.Kind
		go r.execute(ctx, task)
		return nil
	}

	r.runningCount++
	r.running[task.Name] = task.Kind
	ctx := r.ctx
	go r.execute(ctx, task)
	return nil
}

func (r *Runtime) enqueueJobLocked(partition *jobPartition, task Task, blockOnQueue bool) error {
	for {
		if r.finished {
			return ErrRuntimeAlreadyEnded
		}
		if r.closed {
			return ErrRuntimeShuttingDown
		}
		if partition.queueCap <= 0 || len(partition.queue) < partition.queueCap {
			r.enqueueQueuedJobLocked(partition, task)
			partition.cond.Signal()
			return nil
		}

		switch partition.queueFullPolicy {
		case QueueFullReject:
			return ErrJobQueueFull
		case QueueFullDropOldest:
			r.dropQueuedJobLocked(partition, r.findOldestQueuedJobIndexLocked(partition.queue))
			r.enqueueQueuedJobLocked(partition, task)
			partition.cond.Signal()
			return nil
		case QueueFullDropNewest:
			r.dropQueuedJobLocked(partition, r.findNewestQueuedJobIndexLocked(partition.queue))
			r.enqueueQueuedJobLocked(partition, task)
			partition.cond.Signal()
			return nil
		default:
			if !blockOnQueue {
				return ErrJobQueueFull
			}
			partition.cond.Wait()
		}
	}
}

func (r *Runtime) enqueueQueuedJobLocked(partition *jobPartition, task Task) {
	queued := queuedJob{task: task, seq: partition.enqueueSeq}
	partition.enqueueSeq++

	insertAt := len(partition.queue)
	for i, queuedTask := range partition.queue {
		if queued.task.Priority > queuedTask.task.Priority {
			insertAt = i
			break
		}
	}

	partition.queue = append(partition.queue, queuedJob{})
	copy(partition.queue[insertAt+1:], partition.queue[insertAt:])
	partition.queue[insertAt] = queued
}

func (r *Runtime) findOldestQueuedJobIndexLocked(queue []queuedJob) int {
	oldestIndex := 0
	oldestSeq := queue[0].seq
	for i := 1; i < len(queue); i++ {
		if queue[i].seq < oldestSeq {
			oldestSeq = queue[i].seq
			oldestIndex = i
		}
	}
	return oldestIndex
}

func (r *Runtime) findNewestQueuedJobIndexLocked(queue []queuedJob) int {
	newestIndex := 0
	newestSeq := queue[0].seq
	for i := 1; i < len(queue); i++ {
		if queue[i].seq > newestSeq {
			newestSeq = queue[i].seq
			newestIndex = i
		}
	}
	return newestIndex
}

func (r *Runtime) dropQueuedJobLocked(partition *jobPartition, index int) {
	dropped := partition.queue[index].task
	last := len(partition.queue) - 1
	copy(partition.queue[index:], partition.queue[index+1:])
	partition.queue[last] = queuedJob{}
	partition.queue = partition.queue[:last]

	delete(r.running, dropped.Name)
	r.runningCount--
}

func (r *Runtime) jobWorker(partition *jobPartition) {
	for {
		r.mu.Lock()
		for len(partition.queue) == 0 && !r.finished {
			partition.cond.Wait()
		}
		if len(partition.queue) == 0 && r.finished {
			r.mu.Unlock()
			return
		}
		queued := partition.queue[0]
		partition.queue[0] = queuedJob{}
		partition.queue = partition.queue[1:]
		partition.cond.Broadcast()
		ctx := r.ctx
		r.mu.Unlock()

		r.execute(ctx, queued.task)
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
		r.broadcastJobPartitionsLocked()
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
		r.broadcastJobPartitionsLocked()
		r.mu.Unlock()
	})
}

func (r *Runtime) ensureJobPartition(name string) *jobPartition {
	if r.jobPartitions == nil {
		r.jobPartitions = make(map[string]*jobPartition)
	}
	normalized := normalizeJobPartition(name)
	if partition, ok := r.jobPartitions[normalized]; ok {
		return partition
	}

	partition := &jobPartition{
		name: normalized,
		jobPartitionConfig: jobPartitionConfig{
			queueFullPolicy: QueueFullBlock,
		},
	}
	if normalized == DefaultJobPartition {
		partition.workers = goruntime.GOMAXPROCS(0)
	}
	partition.cond = sync.NewCond(&r.mu)
	r.jobPartitions[normalized] = partition
	return partition
}

func normalizeJobPartition(name string) string {
	if name == "" || name == DefaultJobPartition {
		return DefaultJobPartition
	}
	return name
}

func (r *Runtime) validateJobPartitionLocked(task Task) error {
	if task.Kind != KindJob {
		return nil
	}
	_, err := r.jobPartitionForTaskLocked(task)
	return err
}

func (r *Runtime) jobPartitionForTaskLocked(task Task) (*jobPartition, error) {
	normalized := normalizeJobPartition(task.Partition)
	partition, exists := r.jobPartitions[normalized]
	if exists {
		return partition, nil
	}
	partitionName := task.Partition
	if partitionName == "" {
		partitionName = normalized
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownJobPartition, partitionName)
}

func (r *Runtime) broadcastJobPartitionsLocked() {
	for _, partition := range r.jobPartitions {
		partition.cond.Broadcast()
	}
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
