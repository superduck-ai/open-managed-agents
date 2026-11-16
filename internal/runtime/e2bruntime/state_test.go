package e2bruntime

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

func TestGetStatusDoesNotConnectOrResume(t *testing.T) {
	for _, state := range []string{"missing", "paused", "running"} {
		t.Run(state, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/sandboxes/sandbox" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if state == "missing" {
					http.Error(w, `{"message":"not found"}`, 404)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"sandboxID":"sandbox","state":%q,"templateID":"test","metadata":{"workspace_id":"workspace"},"startedAt":"2026-11-16T00:00:00Z","endAt":"2026-11-16T00:01:00Z"}`, state)
			}))
			defer server.Close()
			provider := NewProvider(config.E2BConfig{APIKey: "e2b_0000000000000000000000000000000000000000", APIURL: server.URL, RequestTimeout: time.Second})
			got, err := provider.GetStatus(t.Context(), "sandbox")
			if state == "missing" {
				if !errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil || got.State != state || got.WorkspaceUUID != "workspace" {
				t.Fatalf("state=%+v error=%v", got, err)
			}
			if requests != 1 {
				t.Fatalf("requests=%d", requests)
			}
		})
	}
}

func TestRenewTimeoutNeverConnects(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/sandboxes/sandbox/timeout" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	provider := NewProvider(config.E2BConfig{APIKey: "e2b_0000000000000000000000000000000000000000", APIURL: server.URL, RequestTimeout: time.Second})
	if err := provider.RenewTimeout(t.Context(), "sandbox", time.Minute); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}
