package async

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func TestNewAsync(t *testing.T) {
	type args struct {
		ctx context.Context
		ch  <-chan os.Signal
	}
	tests := []struct {
		name string
		args args
		want *Async
	}{
		{
			name: "new",
			args: args{
				ctx: context.Background(),
				ch:  make(chan os.Signal),
			},
			want: NewAsync(context.Background(), make(chan os.Signal)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewAsync(tt.args.ctx, tt.args.ch); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewAsync() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAsync_RegisterOnShutdown(t *testing.T) {
	asy := NewAsync(context.Background(), make(chan os.Signal))
	asy.RegisterOnShutdown(func(s os.Signal) {
		return
	})
	if len(asy.onShutdown) != 1 {
		t.Error("RegisterOnShutdown() Shutdown register error")
	}
}
