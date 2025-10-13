package async

import (
	"context"
	"testing"
)

func TestNewAsync(t *testing.T) {
	// Simple construction test: NewAsync should return non-nil
	if got := NewAsync(context.Background()); got == nil {
		t.Errorf("NewAsync() returned nil")
	}
}

func TestAsync_RegisterOnShutdown(t *testing.T) {
	asy := NewAsync(context.Background())
	asy.RegisterOnShutdown(func(ctx context.Context) {
		return
	})
	if len(asy.onShutdown) != 1 {
		t.Error("RegisterOnShutdown() Shutdown register error")
	}
}
