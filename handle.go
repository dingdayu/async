package async

import (
	"os"
)

type Handle interface {
	Name() string // Don’t repeat, it will result in replacement
	Handle(ctx Context)
	OnPreRun()              // Before run, panic panic causes registration failure
	OnShutdown(s os.Signal) // On Shutdown
}
