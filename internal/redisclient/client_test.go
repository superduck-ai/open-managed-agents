package redisclient

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestOpenHandshakeTimeoutClosesConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	closed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			closed <- err
			return
		}
		t.Cleanup(func() { _ = conn.Close() })
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			closed <- err
			return
		}
		_, err = io.Copy(io.Discard, conn)
		closed <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	client, err := Open(ctx, "redis://"+listener.Addr().String()+"?max_retries=-1")
	if client != nil {
		t.Cleanup(func() { _ = client.Close() })
		t.Fatal("unresponsive Redis accepted")
	}
	var timeout net.Error
	if !errors.Is(err, context.DeadlineExceeded) && !(errors.As(err, &timeout) && timeout.Timeout()) {
		t.Fatalf("expected caller timeout, got %v", err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("peer did not receive EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed Redis initialization retained its socket")
	}
}

func TestOpenAuthFailureClosesConnections(t *testing.T) {
	server := miniredis.RunT(t)
	server.RequireAuth("correct-password")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	client, err := Open(ctx, "redis://:wrong-password@"+server.Addr()+"?max_retries=-1")
	if client != nil {
		t.Cleanup(func() { _ = client.Close() })
		t.Fatal("invalid Redis credentials accepted")
	}
	if err == nil || !strings.Contains(err.Error(), "WRONGPASS") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for server.CurrentConnectionCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("rejected authentication retained its socket")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestOpenSharesHealthyConnections(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := Open(t.Context(), "redis://"+server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Set(t.Context(), "healthy", "value", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if value, err := client.Get(t.Context(), "healthy").Result(); err != nil || value != "value" {
		t.Fatalf("read healthy connection: %q %v", value, err)
	}
	if !client.Options().ContextTimeoutEnabled {
		t.Fatal("caller deadlines disabled")
	}
}
