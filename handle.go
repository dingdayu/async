package async

import (
	"context"
)

type Handle interface {
	Name() string
	Handle(ctx Context)
	OnPreRun()
	OnShutdown(ctx context.Context)
}
