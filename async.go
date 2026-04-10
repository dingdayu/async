package async

import (
	"container/heap"
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
	mu         sync.Mutex
	queue      queuedJobHeap
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
	activeCount  int
	firstErr     error
	observers    []Observer

	jobPartitions map[string]*jobPartition
}

type queuedJob struct {
	task Task
	seq  uint64
}

type queuedJobHeap []queuedJob

func (h queuedJobHeap) Len() int {
	return len(h)
}

func (h queuedJobHeap) Less(i, j int) bool {
	if h[i].task.Priority != h[j].task.Priority {
		return h[i].task.Priority > h[j].task.Priority
	}
	return h[i].seq < h[j].seq
}

func (h queuedJobHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *queuedJobHeap) Push(x interface{}) {
	*h = append(*h, x.(queuedJob))
}

func (h *queuedJobHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = queuedJob{}
	*h = old[:n-1]
	return item
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
	defaultPartition.cond = sync.NewCond(&defaultPartition.mu)
	heap.Init(&defaultPartition.queue)
	for _, opt := range opts {
		opt(r)
	}
	for _, partition := range r.jobPartitions {
		if partition.cond == nil {
			partition.cond = sync.NewCond(&partition.mu)
		}
		heap.Init(&partition.queue)
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

	events := []Event{{
		Type:      EventTaskAdded,
		Task:      task,
		Partition: normalizeJobPartition(task.Partition),
	}}

	r.mu.Lock()

	if r.finished {
		r.mu.Unlock()
		return ErrRuntimeAlreadyEnded
	}
	if r.closed {
		r.mu.Unlock()
		return ErrRuntimeShuttingDown
	}
	if _, exists := r.pending[task.Name]; exists {
		r.mu.Unlock()
		return ErrTaskAlreadyExists
	}
	if _, exists := r.running[task.Name]; exists {
		r.mu.Unlock()
		return ErrTaskAlreadyExists
	}
	partition, err := r.validateJobPartitionLocked(task)
	if err != nil {
		r.mu.Unlock()
		return err
	}

	if !r.started {
		r.pending[task.Name] = task
		r.mu.Unlock()
		r.emitAll(events...)
		return nil
	}
	r.reserveTaskLocked(task)
	r.mu.Unlock()

	launchEvents, err := r.launchReservedTask(task, partition, blockOnQueue)
	if err != nil {
		r.rollbackReservedTask(task.Name)
		return err
	}
	r.emitAll(append(events, launchEvents...)...)
	return nil
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

	events := []Event{{Type: EventRuntimeStarted}}
	r.started = true
	r.ctx, r.cancel = context.WithCancel(parent)
	r.startJobWorkersLocked()
	pendingTasks := make([]Task, 0, len(r.pending))
	for _, task := range r.pending {
		pendingTasks = append(pendingTasks, task)
	}
	r.pending = make(map[string]Task)
	r.mu.Unlock()

	for _, task := range pendingTasks {
		r.mu.Lock()
		partition, err := r.validateJobPartitionLocked(task)
		if err != nil {
			r.pending[task.Name] = task
			r.mu.Unlock()
			break
		}
		r.reserveTaskLocked(task)
		r.mu.Unlock()

		launchEvents, err := r.launchReservedTask(task, partition, true)
		if err != nil {
			r.rollbackReservedTask(task.Name)
			r.mu.Lock()
			r.pending[task.Name] = task
			r.mu.Unlock()
			break
		}
		events = append(events, launchEvents...)
	}

	r.mu.Lock()

	if r.runningCount == 0 {
		r.finished = true
	}
	finished := r.finished
	r.mu.Unlock()
	r.emitAll(events...)

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

func (r *Runtime) launchReservedTask(task Task, partition *jobPartition, blockOnQueue bool) ([]Event, error) {
	if task.Kind == KindJob && partition != nil && partition.workers > 0 {
		return r.enqueueJob(partition, task, blockOnQueue)
	}

	r.mu.Lock()
	r.activeCount++
	r.mu.Unlock()
	go r.execute(r.ctx, task)
	return nil, nil
}

func (r *Runtime) enqueueJob(partition *jobPartition, task Task, blockOnQueue bool) ([]Event, error) {
	events := make([]Event, 0, 2)
	for {
		r.mu.Lock()
		if r.finished {
			r.mu.Unlock()
			return nil, ErrRuntimeAlreadyEnded
		}
		if r.closed {
			r.mu.Unlock()
			return nil, ErrRuntimeShuttingDown
		}
		partition.mu.Lock()
		if partition.queueCap <= 0 || partition.queue.Len() < partition.queueCap {
			r.enqueueQueuedJobLocked(partition, task)
			partition.cond.Signal()
			partition.mu.Unlock()
			r.mu.Unlock()
			events = append(events, Event{Type: EventTaskQueued, Task: task, Partition: partition.name})
			return events, nil
		}

		switch partition.queueFullPolicy {
		case QueueFullReject:
			partition.mu.Unlock()
			r.mu.Unlock()
			return nil, ErrJobQueueFull
		case QueueFullDropOldest:
			dropped := r.dropQueuedJobLocked(partition, r.findOldestQueuedJobIndexLocked(partition.queue))
			events = append(events, Event{Type: EventTaskDropped, Task: dropped, Partition: partition.name, QueueFullPolicy: partition.queueFullPolicy})
			r.enqueueQueuedJobLocked(partition, task)
			partition.cond.Signal()
			partition.mu.Unlock()
			r.mu.Unlock()
			events = append(events, Event{Type: EventTaskQueued, Task: task, Partition: partition.name})
			return events, nil
		case QueueFullDropNewest:
			dropped := r.dropQueuedJobLocked(partition, r.findNewestQueuedJobIndexLocked(partition.queue))
			events = append(events, Event{Type: EventTaskDropped, Task: dropped, Partition: partition.name, QueueFullPolicy: partition.queueFullPolicy})
			r.enqueueQueuedJobLocked(partition, task)
			partition.cond.Signal()
			partition.mu.Unlock()
			r.mu.Unlock()
			events = append(events, Event{Type: EventTaskQueued, Task: task, Partition: partition.name})
			return events, nil
		default:
			if !blockOnQueue {
				partition.mu.Unlock()
				r.mu.Unlock()
				return nil, ErrJobQueueFull
			}
			r.mu.Unlock()
			partition.cond.Wait()
			partition.mu.Unlock()
			continue
		}
	}
}

func (r *Runtime) enqueueQueuedJobLocked(partition *jobPartition, task Task) {
	queued := queuedJob{task: task, seq: partition.enqueueSeq}
	partition.enqueueSeq++
	heap.Push(&partition.queue, queued)
}

func (r *Runtime) findOldestQueuedJobIndexLocked(queue queuedJobHeap) int {
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

func (r *Runtime) findNewestQueuedJobIndexLocked(queue queuedJobHeap) int {
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

func (r *Runtime) dropQueuedJobLocked(partition *jobPartition, index int) Task {
	dropped := heap.Remove(&partition.queue, index).(queuedJob).task

	delete(r.running, dropped.Name)
	r.runningCount--
	return dropped
}

func (r *Runtime) jobWorker(partition *jobPartition) {
	for {
		partition.mu.Lock()
		for partition.queue.Len() == 0 {
			if r.waitClosed() {
				partition.mu.Unlock()
				return
			}
			partition.cond.Wait()
		}
		queued := heap.Pop(&partition.queue).(queuedJob)
		partition.cond.Broadcast()
		partition.mu.Unlock()

		r.mu.Lock()
		r.activeCount++
		r.mu.Unlock()
		r.execute(r.ctx, queued.task)
	}
}

func (r *Runtime) waitClosed() bool {
	select {
	case <-r.waitCh:
		return true
	default:
		return false
	}
}

func (r *Runtime) execute(ctx context.Context, task Task) {
	r.emitAll(Event{Type: EventTaskStarted, Task: task, Partition: normalizeJobPartition(task.Partition)})
	err := runTask(ctx, task)

	r.mu.Lock()
	delete(r.running, task.Name)
	r.runningCount--
	r.activeCount--

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
	r.emitAll(Event{Type: EventTaskFinished, Task: task, Partition: normalizeJobPartition(task.Partition), Err: err})

	if cancel != nil {
		cancel()
	}
	if finished {
		r.closeWait()
	}
}

func (r *Runtime) closeWait() {
	closed := false
	var err error
	r.closeOnce.Do(func() {
		close(r.waitCh)
		r.mu.Lock()
		r.broadcastJobPartitionsLocked()
		err = r.firstErr
		r.mu.Unlock()
		closed = true
	})
	if closed {
		r.emitAll(Event{Type: EventRuntimeFinished, Err: err})
	}
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
	partition.cond = sync.NewCond(&partition.mu)
	heap.Init(&partition.queue)
	r.jobPartitions[normalized] = partition
	return partition
}

func normalizeJobPartition(name string) string {
	if name == "" || name == DefaultJobPartition {
		return DefaultJobPartition
	}
	return name
}

func (r *Runtime) validateJobPartitionLocked(task Task) (*jobPartition, error) {
	if task.Kind != KindJob {
		return nil, nil
	}
	return r.jobPartitionForTaskLocked(task)
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
		partition.mu.Lock()
		partition.cond.Broadcast()
		partition.mu.Unlock()
	}
}

func (r *Runtime) reserveTaskLocked(task Task) {
	r.runningCount++
	r.running[task.Name] = task.Kind
}

func (r *Runtime) rollbackReservedTask(name string) {
	r.mu.Lock()
	delete(r.running, name)
	r.runningCount--
	if r.runningCount == 0 {
		r.finished = true
	}
	finished := r.finished
	r.mu.Unlock()
	if finished {
		r.closeWait()
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
