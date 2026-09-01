package e2bruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestKillUsesExplicitSDKAPIURLWithoutConnecting(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable, http.StatusNotFound, http.StatusNoContent} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodDelete || r.URL.Path != "/sandboxes/sbx_paused" {
					t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("X-API-Key") != "e2b_0000000000000000000000000000000000000000" || r.Header.Get("Authorization") != "Bearer test-access-token" {
					t.Error("provider request did not retain configured authentication")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			provider := NewProvider(config.E2BConfig{
				APIKey:      "e2b_0000000000000000000000000000000000000000",
				AccessToken: "test-access-token", APIURL: server.URL, Debug: true, RequestTimeout: time.Second,
			})
			err := provider.Kill(context.Background(), "sbx_paused")
			if (err != nil) != (status == http.StatusUnauthorized || status == http.StatusServiceUnavailable) {
				t.Fatalf("Kill error = %v at status %d", err, status)
			}
			if calls.Load() != 1 {
				t.Fatalf("provider requests = %d, want one DELETE", calls.Load())
			}
		})
	}
}
