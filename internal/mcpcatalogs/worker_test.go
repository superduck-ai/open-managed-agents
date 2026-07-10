package mcpcatalogs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeWorkerStore struct {
	completed []db.CompleteMCPToolDiscoveryInput
	failed    []db.FailMCPToolDiscoveryInput
	leases    []time.Duration
}

func (s *fakeWorkerStore) LeaseMCPToolDiscoveryJobs(_ context.Context, _ string, _ int, lease time.Duration) ([]db.MCPToolDiscoveryJob, error) {
	s.leases = append(s.leases, lease)
	return nil, nil
}

func (s *fakeWorkerStore) CompleteMCPToolDiscovery(_ context.Context, input db.CompleteMCPToolDiscoveryInput) error {
	s.completed = append(s.completed, input)
	return nil
}

func (s *fakeWorkerStore) FailMCPToolDiscovery(_ context.Context, input db.FailMCPToolDiscoveryInput) error {
	s.failed = append(s.failed, input)
	return nil
}

func (*fakeWorkerStore) RunMCPToolCatalogRetention(context.Context, time.Time) error { return nil }

func TestWorkerLeaseCoversConfiguredProbeTimeout(t *testing.T) {
	store := &fakeWorkerStore{}
	probeTimeout := 2 * time.Minute
	worker := NewWorker(store, config.Config{MCPDiscoveryProbeTimeout: probeTimeout})
	if err := worker.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if len(store.leases) != 1 {
		t.Fatalf("lease calls = %d, want 1", len(store.leases))
	}
	if got, minimum := store.leases[0], probeTimeout+discoveryLeaseCleanupMargin; got < minimum {
		t.Fatalf("lease duration = %v, want at least %v", got, minimum)
	}
}

func TestWorkerPersistsSafeTerminalFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "credential rejected: secret-value", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	store := &fakeWorkerStore{}
	worker := NewWorker(store, config.Config{MCPDiscoveryProbeTimeout: time.Second})
	worker.process(context.Background(), db.MCPToolDiscoveryJob{
		ID: 1, WorkspaceID: 2, CatalogExternalID: "mcpc_test", Generation: 1, EndpointURL: server.URL,
	})

	if len(store.failed) != 1 {
		t.Fatalf("worker failures = %d, want 1", len(store.failed))
	}
	if got := store.failed[0]; got.ErrorCode != "auth_required" || got.Retryable || got.ErrorMessage == "credential rejected: secret-value" {
		t.Fatalf("worker failure = %#v", got)
	}
}

func TestWorkerPersistsDiscoveredCatalog(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "weather-service", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_forecast", Description: "Returns a forecast."}, emptyToolHandler)
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	store := &fakeWorkerStore{}
	worker := NewWorker(store, config.Config{MCPDiscoveryProbeTimeout: time.Second})
	worker.process(context.Background(), db.MCPToolDiscoveryJob{
		ID: 1, WorkspaceID: 2, CatalogExternalID: "mcpc_test", Generation: 3, EndpointURL: httpServer.URL,
	})

	if len(store.completed) != 1 {
		t.Fatalf("worker completions = %d, want 1", len(store.completed))
	}
	got := store.completed[0]
	if got.Generation != 3 || got.CatalogHash == "" || string(got.Tools) != `[{"name":"get_forecast","description":"Returns a forecast."}]` {
		t.Fatalf("worker completion = %#v", got)
	}
	if !got.ExpiresAt.After(got.DiscoveredAt) {
		t.Fatalf("catalog expiry %v must follow discovery %v", got.ExpiresAt, got.DiscoveredAt)
	}
}
