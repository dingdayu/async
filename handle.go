package async

import (
	"context"
	"os"
	"sync"
)

// Handle
type Handle interface {
	Name() string // Don’t repeat, it will result in replacement
	Handle(ctx context.Context, wg *sync.WaitGroup)
	OnPreRun()              // Before run, panic panic causes registration failure
	OnShutdown(s os.Signal) // On Shutdown
}
