package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamContextCancellation(t *testing.T) {
	for _, source := range []string{"shutdown", "request", "already_shutdown"} {
		t.Run(source, func(t *testing.T) {
			shutdown, stop := context.WithCancel(t.Context())
			defer stop()
			request, disconnect := context.WithCancel(t.Context())
			defer disconnect()
			if source == "already_shutdown" {
				stop()
			}
			handler := WithStreamShutdown(shutdown, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				ctx, cancel := StreamContext(r.Context())
				defer cancel()
				if source == "request" {
					disconnect()
				} else {
					stop()
				}
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
					t.Fatal("stream context was not canceled")
				}
				if source != "request" && r.Context().Err() != nil {
					t.Fatal("shutdown canceled the ordinary request context")
				}
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(request))
		})
	}
}

func TestStreamContextWithoutShutdown(t *testing.T) {
	ctx, cancel := StreamContext(WithRequestID(t.Context(), "req_stream"))
	defer cancel()
	if ctx.Err() != nil || RequestID(ctx) != "req_stream" {
		t.Fatal("stream context did not preserve the request context")
	}
}
