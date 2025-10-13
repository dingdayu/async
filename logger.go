package async

import (
	"log/slog"
)

// newDefaultLogger returns a default structured logger. Users can override by WithLogger.
func newDefaultLogger() *slog.Logger {
	return slog.Default()
}

// helper to ensure logger not nil
func ensureLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return newDefaultLogger()
	}
	return l
}
