package liveworker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCleanupRequestSurvivesTestContextCancellation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/agents/agent_test/archive" || r.URL.Query().Get("beta") != "true" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected archive request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	env := &liveEnv{url: server.URL}
	t.Run("cleanup", func(t *testing.T) {
		t.Cleanup(func() {
			if t.Context().Err() == nil {
				t.Fatal("test context must already be canceled during cleanup")
			}
			env.requestContext(context.WithoutCancel(t.Context()), t, http.MethodPost, "/v1/agents/agent_test/archive", "test-key", nil, http.StatusOK)
		})
	})
	if requests != 1 {
		t.Fatalf("archive requests = %d, want 1", requests)
	}
}
