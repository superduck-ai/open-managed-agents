package httpapi

import (
	"context"
	"net/http"
)

type streamShutdownKey struct{}

func WithStreamShutdown(shutdown context.Context, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), streamShutdownKey{}, shutdown)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func StreamContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	shutdown, ok := parent.Value(streamShutdownKey{}).(context.Context)
	if !ok {
		return ctx, cancel
	}
	stop := context.AfterFunc(shutdown, cancel)
	if shutdown.Err() != nil {
		cancel()
	}
	return ctx, func() {
		stop()
		cancel()
	}
}
