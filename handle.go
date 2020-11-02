package async

import (
	"context"
	"os"
	"sync"
)

// Shutdown async shutdown func
type Shutdown func(s os.Signal)

type Handle interface {
	Name() string
	Handle(ctx context.Context, wg *sync.WaitGroup)
	OnShutdown(s os.Signal)
}
