package liveworker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestChatShutdown(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	f := e.newSession(t)
	t.Run("deadline_closes_stalled_request", func(t *testing.T) {
		peer := startChatPeer(t)
		connection := stallChatPeerRequest(t, peer, f)
		defer connection.Close()
		stopping := time.Now()
		requireOK(t, peer.command.Process.Signal(syscall.SIGTERM))
		err := waitChatPeerExit(t, peer, 15*time.Second)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("shutdown exit = %v, want exit 1", err)
		}
		elapsed := time.Since(stopping)
		if elapsed < 10*time.Second {
			t.Fatalf("stalled request did not receive its drain budget: %s", elapsed)
		}
		requireOK(t, connection.SetReadDeadline(time.Now().Add(time.Second)))
		data, err := io.ReadAll(connection)
		if err != nil {
			t.Fatalf("connection remained open after shutdown: %v", err)
		}
		if len(data) != 0 {
			t.Fatalf("unexpected response after forced close: %q", data)
		}
		t.Logf("shutdown deadline: elapsed=%s exit_code=%d", elapsed, exit.ExitCode())
	})
	if t.Failed() {
		return
	}
	chatProof(t, started, "shutdown_deadline_enforced")
	t.Run("second_interrupt_terminates_wait", func(t *testing.T) {
		peer := startChatPeer(t)
		connection := stallChatPeerRequest(t, peer, f)
		defer connection.Close()
		requireOK(t, peer.command.Process.Signal(os.Interrupt))
		waitRealWorker(t, "peer listener closed", func() bool {
			connection, err := net.DialTimeout("tcp", strings.TrimPrefix(peer.url, "http://"), time.Second)
			if err != nil {
				return true
			}
			_ = connection.Close()
			return false
		})
		stopping := time.Now()
		requireOK(t, peer.command.Process.Signal(os.Interrupt))
		err := waitChatPeerExit(t, peer, 3*time.Second)
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("second interrupt exit = %v", err)
		}
		status, ok := exit.Sys().(syscall.WaitStatus)
		if !ok || status.Signal() != syscall.SIGINT {
			t.Fatalf("second interrupt status = %v", exit.Sys())
		}
		t.Logf("second interrupt: elapsed=%s signal=%s", time.Since(stopping), status.Signal())
	})
	if t.Failed() {
		return
	}
	chatProof(t, started, "second_interrupt_exits")
	t.Run("active_streams_close_cleanly", func(t *testing.T) {
		peer := startChatPeer(t)
		remoteEnv := *e
		remoteEnv.url = peer.url
		remoteSession := *f
		remoteSession.env = &remoteEnv
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		events, closeStream := connectChatStream(t, ctx, &remoteSession)
		defer closeStream()
		worker := remoteSession.connect(t, "")
		defer worker.cancel()
		stopping := time.Now()
		requireOK(t, peer.command.Process.Signal(os.Interrupt))
		requireOK(t, waitChatPeerExit(t, peer, 5*time.Second))
		for range events {
		}
		select {
		case err := <-worker.errors:
			requireOK(t, err)
		case <-ctx.Done():
			t.Fatal("worker stream did not close")
		}
		current, _, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
		requireOK(t, err)
		if current.CurrentWorkerEpoch != f.code.CurrentWorkerEpoch || current.Status != "active" {
			t.Fatal("backend shutdown retired the active worker")
		}
		t.Logf("active public and worker SSE: elapsed=%s exit_code=0", time.Since(stopping))
	})
	if !t.Failed() {
		chatProof(t, started, "active_streams_shutdown_cleanly")
	}
}

func stallChatPeerRequest(t *testing.T, peer *chatPeer, session *liveSession) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", strings.TrimPrefix(peer.url, "http://"), time.Second)
	requireOK(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	requireOK(t, connection.SetDeadline(time.Now().Add(20*time.Second)))
	_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 1000\r\nExpect: 100-continue\r\n\r\n", session.path("/worker/internal-events"), strings.TrimPrefix(peer.url, "http://"), session.token)
	requireOK(t, err)
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	requireOK(t, err)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusContinue {
		t.Fatalf("stalled request status = %d, want 100 Continue", response.StatusCode)
	}
	return connection
}

func waitChatPeerExit(t *testing.T, peer *chatPeer, timeout time.Duration) error {
	t.Helper()
	select {
	case <-peer.done:
		return peer.exitErr
	case <-time.After(timeout):
		t.Fatal("backend did not exit within shutdown budget")
		return nil
	}
}
