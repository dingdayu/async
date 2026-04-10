package async

// EventType identifies a runtime lifecycle event observed by an Observer.
type EventType string

const (
	EventRuntimeStarted  EventType = "runtime_started"
	EventRuntimeFinished EventType = "runtime_finished"
	EventTaskAdded       EventType = "task_added"
	EventTaskQueued      EventType = "task_queued"
	EventTaskStarted     EventType = "task_started"
	EventTaskFinished    EventType = "task_finished"
	EventTaskDropped     EventType = "task_dropped"
)

// Event reports a runtime or task lifecycle transition.
type Event struct {
	Type            EventType
	Task            Task
	Err             error
	Partition       string
	QueueFullPolicy QueueFullPolicy
}

// Observer receives runtime lifecycle events.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe calls f(event).
func (f ObserverFunc) Observe(event Event) {
	if f != nil {
		f(event)
	}
}

// WithObserver registers one or more observers that receive runtime events.
func WithObserver(observers ...Observer) Option {
	return func(r *Runtime) {
		for _, observer := range observers {
			if observer != nil {
				r.observers = append(r.observers, observer)
			}
		}
	}
}

// JobPartitionStats is a point-in-time view of one job partition.
type JobPartitionStats struct {
	Workers         int
	QueueCap        int
	QueueLen        int
	QueueFullPolicy QueueFullPolicy
}

// RuntimeStats is a point-in-time view of runtime state.
type RuntimeStats struct {
	Started    bool
	Closed     bool
	Finished   bool
	Pending    int
	Running    int
	FirstError error
	Partitions map[string]JobPartitionStats
}

// Stats returns a snapshot of runtime state and partition queue usage.
func (r *Runtime) Stats() RuntimeStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := RuntimeStats{
		Started:    r.started,
		Closed:     r.closed,
		Finished:   r.finished,
		Pending:    len(r.pending),
		Running:    r.activeCount,
		FirstError: r.firstErr,
		Partitions: make(map[string]JobPartitionStats, len(r.jobPartitions)),
	}

	for name, partition := range r.jobPartitions {
		partition.mu.Lock()
		stats.Partitions[name] = JobPartitionStats{
			Workers:         partition.workers,
			QueueCap:        partition.queueCap,
			QueueLen:        partition.queue.Len(),
			QueueFullPolicy: partition.queueFullPolicy,
		}
		partition.mu.Unlock()
	}

	return stats
}

func (r *Runtime) emitAll(events ...Event) {
	if len(events) == 0 {
		return
	}

	r.mu.Lock()
	observers := append([]Observer(nil), r.observers...)
	r.mu.Unlock()
	if len(observers) == 0 {
		return
	}

	for _, event := range events {
		for _, observer := range observers {
			func() {
				defer func() {
					_ = recover()
				}()
				observer.Observe(event)
			}()
		}
	}
}
