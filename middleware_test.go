package async

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestWithTaskMiddlewareWrapsRuntimeTasks(t *testing.T) {
	var order []string
	rt := NewRuntime(
		WithTaskMiddleware(
			func(task Task, next Runner) Runner {
				return func(ctx context.Context) error {
					order = append(order, "runtime-before-1:"+task.Name)
					err := next(ctx)
					order = append(order, "runtime-after-1:"+task.Name)
					return err
				}
			},
			func(task Task, next Runner) Runner {
				return func(ctx context.Context) error {
					order = append(order, "runtime-before-2:"+task.Name)
					err := next(ctx)
					order = append(order, "runtime-after-2:"+task.Name)
					return err
				}
			},
		),
	)

	if err := rt.Add(Job("job", func(ctx context.Context) error {
		order = append(order, "runner:job")
		return nil
	})); err != nil {
		t.Fatalf("add job: %v", err)
	}

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{
		"runtime-before-1:job",
		"runtime-before-2:job",
		"runner:job",
		"runtime-after-2:job",
		"runtime-after-1:job",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("middleware order = %#v, want %#v", order, want)
	}
}

func TestTaskWithMiddlewareWrapsSingleTask(t *testing.T) {
	var order []string
	rt := NewRuntime()

	task := Job("job", func(ctx context.Context) error {
		order = append(order, "runner")
		return nil
	}).WithMiddleware(func(task Task, next Runner) Runner {
		return func(ctx context.Context) error {
			order = append(order, "task-before:"+task.Name)
			err := next(ctx)
			order = append(order, "task-after:"+task.Name)
			return err
		}
	})

	if err := rt.Add(task); err != nil {
		t.Fatalf("add task: %v", err)
	}

	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{"task-before:job", "runner", "task-after:job"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("middleware order = %#v, want %#v", order, want)
	}
}

func TestTaskMiddlewareUsesFinalTaskMetadata(t *testing.T) {
	var gotName string
	var gotPartition string
	var gotPriority int

	rt := NewRuntime()
	task := Job("job", func(ctx context.Context) error { return nil }).
		WithMiddleware(func(task Task, next Runner) Runner {
			return func(ctx context.Context) error {
				gotName = task.Name
				gotPartition = task.Partition
				gotPriority = task.Priority
				return next(ctx)
			}
		}).
		WithPartition("batch").
		WithPriority(9)

	if err := rt.Add(task); err == nil {
		t.Fatalf("expected unknown partition error")
	}

	rt = NewRuntime(WithJobPartition("batch", JobPartitionConfig{Workers: 0}))
	if err := rt.Add(task); err != nil {
		t.Fatalf("add task: %v", err)
	}
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if gotName != "job" || gotPartition != "batch" || gotPriority != 9 {
		t.Fatalf("middleware saw stale task metadata: name=%q partition=%q priority=%d", gotName, gotPartition, gotPriority)
	}
}

func TestObserverPanicDoesNotCrashRuntime(t *testing.T) {
	rt := NewRuntime(WithObserver(ObserverFunc(func(Event) { panic("boom") })))
	if err := rt.Add(Job("job", func(ctx context.Context) error { return nil })); err != nil {
		t.Fatalf("add task: %v", err)
	}
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestStatsRunningCountsExecutingOnly(t *testing.T) {
	rt := NewRuntime(WithJobPool(1), WithJobQueue(1))
	started := make(chan struct{})
	release := make(chan struct{})

	if err := rt.Add(Job("job-1", func(ctx context.Context) error {
		close(started)
		<-release
		return nil
	})); err != nil {
		t.Fatalf("add job-1: %v", err)
	}
	if err := rt.Add(Job("job-2", func(ctx context.Context) error { return nil })); err != nil {
		t.Fatalf("add job-2: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("job-1 did not start")
	}

	deadline := time.Now().Add(time.Second)
	for {
		stats := rt.Stats()
		if stats.Partitions[DefaultJobPartition].QueueLen == 1 {
			if stats.Running != 1 {
				t.Fatalf("expected Running=1 with one executing and one queued job, got %d", stats.Running)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for queued stats")
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	if err := rt.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}
