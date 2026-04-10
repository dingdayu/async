package async

import "fmt"

// Middleware wraps a task runner while preserving task metadata.
type Middleware func(Task, Runner) Runner

// WithTaskMiddleware registers runtime-wide task middleware.
func WithTaskMiddleware(middlewares ...Middleware) Option {
	return func(r *Runtime) {
		for _, middleware := range middlewares {
			if middleware != nil {
				r.middlewares = append(r.middlewares, middleware)
			}
		}
	}
}

// WrapRunner applies middleware around a runner.
func WrapRunner(task Task, runner Runner, middlewares ...Middleware) Runner {
	wrapped := runner
	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] != nil {
			wrapped = middlewares[i](task, wrapped)
		}
	}
	return wrapped
}

// WrapTask returns a copy of task with middleware applied to its runner.
func WrapTask(task Task, middlewares ...Middleware) Task {
	task.mw = append(append([]Middleware(nil), task.mw...), middlewares...)
	return task
}

func (r *Runtime) wrapTask(task Task) (Task, error) {
	r.mu.Lock()
	runtimeMiddlewares := append([]Middleware(nil), r.middlewares...)
	r.mu.Unlock()

	combined := make([]Middleware, 0, len(runtimeMiddlewares)+len(task.mw))
	combined = append(combined, runtimeMiddlewares...)
	combined = append(combined, task.mw...)
	if len(combined) == 0 {
		return task, nil
	}

	wrapped := task
	wrapped.mw = nil
	defer func() {
		wrapped.mw = nil
	}()

	var wrapErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				wrapErr = fmt.Errorf("middleware setup panic: %v", rec)
			}
		}()
		wrapped.Runner = WrapRunner(wrapped, task.Runner, combined...)
	}()
	if wrapErr != nil {
		return task, wrapErr
	}
	return wrapped, nil
}
