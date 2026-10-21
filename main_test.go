package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/listeners"
	"go.opentelemetry.io/otel/trace"
)

func TestShutdownHTTPDeadlineClosesActiveRequests(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(finished)
	}))
	defer server.Close()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	waitForShutdownEvent(t, started)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if err := shutdownHTTP(ctx, server.Config); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v", err)
	}
	waitForShutdownEvent(t, finished)
	waitForShutdownEvent(t, requestDone)
}

func TestServeHTTPStopsStreamsAndDrainsRequests(t *testing.T) {
	addr, file, err := listeners.Reserve()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	t.Setenv("OMA_HTTP_LISTENER_FD", fmt.Sprint(file.Fd()))
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	streamStarted := make(chan struct{})
	streamFinished := make(chan struct{})
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/stream" {
				streamCtx, cancel := httpapi.StreamContext(r.Context())
				defer cancel()
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				close(streamStarted)
				<-streamCtx.Done()
				close(streamFinished)
				return
			}
			close(requestStarted)
			select {
			case <-releaseRequest:
			case <-streamFinished:
			}
			if r.Context().Err() != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = io.WriteString(w, "completed")
		}),
		ReadHeaderTimeout: time.Second,
	}
	defer func() {
		stop()
		close(releaseRequest)
		_ = server.Close()
	}()
	served := make(chan error, 1)
	go func() { served <- serveHTTP(ctx, server, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + addr + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	waitForShutdownEvent(t, streamStarted)
	requestDone := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + addr + "/request")
		if err == nil {
			defer response.Body.Close()
			var body []byte
			body, err = io.ReadAll(response.Body)
			if err == nil && string(body) != "completed" {
				err = fmt.Errorf("response body = %q", body)
			}
		}
		requestDone <- err
	}()
	waitForShutdownEvent(t, requestStarted)
	stop()
	waitForShutdownEvent(t, streamFinished)
	for _, result := range []<-chan error{requestDone, served} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("HTTP shutdown did not complete")
		}
	}
}

func waitForShutdownEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for shutdown event")
	}
}

func TestLogContextAttrs(t *testing.T) {
	if got := logContextAttrs(context.Background()); len(got) != 0 {
		t.Fatalf("unexpected empty-context attrs: %v", got)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	ctx := trace.ContextWithSpanContext(httpapi.WithRequestID(t.Context(), "req_test"), spanContext)
	got := map[string]string{}
	for _, attr := range logContextAttrs(ctx) {
		got[attr.Key] = attr.Value.String()
	}
	want := map[string]string{"request_id": "req_test", "trace_id": spanContext.TraceID().String(), "span_id": spanContext.SpanID().String()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("context attrs = %v, want %v", got, want)
	}
}
