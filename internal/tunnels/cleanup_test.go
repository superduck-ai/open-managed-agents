package tunnels

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type cleanupDatabase struct {
	tunnel db.MCPTunnel
	err    error
}

func (d cleanupDatabase) GetMCPTunnel(context.Context, string, string, string) (db.MCPTunnel, error) {
	return d.tunnel, d.err
}

func TestControlCleanupProtectsActiveAndMismatchedResources(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	now := time.Now()
	for _, record := range []db.MCPTunnel{{UUID: "tunnel"}, {UUID: "other", ArchivedAt: &now}} {
		worker := controlCleanupWorker{database: cleanupDatabase{tunnel: record}, broker: b}
		if err := worker.cleanup(t.Context(), controlCleanupArgs{TunnelUUID: "tunnel"}); err != nil {
			t.Fatal(err)
		}
		var state tunnelControl
		if _, err := b.control.read(t.Context(), brokerKey("tunnel"), &state); err != nil {
			t.Fatalf("unrelated control removed: %v", err)
		}
	}
}

func TestControlCleanupDefersInfrastructureFailureAndRetries(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	now := time.Now()
	database := cleanupDatabase{tunnel: db.MCPTunnel{UUID: "tunnel", ArchivedAt: &now}}
	worker := controlCleanupWorker{database: database, broker: b, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	serverURL := b.connection.ConnectedUrl()
	b.connection.Close()
	job := &river.Job[controlCleanupArgs]{Args: controlCleanupArgs{TunnelUUID: "tunnel"}}
	var snooze *river.JobSnoozeError
	if err := worker.Work(t.Context(), job); !errors.As(err, &snooze) {
		t.Fatalf("failure was not deferred: %v", err)
	}
	reconnected, err := newBroker(t.Context(), connectTunnelNATS(t, serverURL), b.cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reconnected.Close)
	worker.broker = reconnected
	for range 2 {
		if err := worker.Work(t.Context(), job); err != nil {
			t.Fatal(err)
		}
	}
	var state tunnelControl
	if _, err := reconnected.control.read(t.Context(), brokerKey("tunnel"), &state); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("control not removed: %v", err)
	}
}

// The initial lookup returns a credential that was valid immediately before
// archive committed; recovery must check the current database state again.
type archivedDuringLookupDatabase struct{ connectorMetadataDatabase }

func (d *archivedDuringLookupDatabase) FindMCPTunnelTokenContext(ctx context.Context, id string, hash []byte) (db.MCPTunnelTokenContext, error) {
	credential, err := d.connectorMetadataDatabase.FindMCPTunnelTokenContext(ctx, id, hash)
	now := time.Now()
	d.context.TunnelArchivedAt = &now
	return credential, err
}

func TestMissingControlRecoveryRejectsLateArchivedPoll(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	database := &archivedDuringLookupDatabase{connectorMetadataDatabase{context: credential}}
	h := ConnectorHandler{cfg: b.cfg, db: database, broker: b}
	request := connectorMetadataRequest("valid-token")
	request.URL.RawQuery = "timeout_ms=0"
	if err := h.poll(httptest.NewRecorder(), request); err == nil {
		t.Fatal("late archived poll recovered control")
	}
	var state tunnelControl
	if _, err := b.control.read(t.Context(), brokerKey(credential.Token.TunnelUUID), &state); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("late poll created control: %v", err)
	}
}

func TestMissingControlRecoveryInitializesValidConnector(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	database := connectorMetadataDatabase{context: credential, tunnel: db.MCPTunnel{UUID: credential.Token.TunnelUUID}}
	h := ConnectorHandler{cfg: b.cfg, db: database, broker: b}
	request := connectorMetadataRequest("valid-token")
	request.URL.RawQuery = "timeout_ms=0"
	response := httptest.NewRecorder()
	if err := h.poll(response, request); err != nil {
		t.Fatal(err)
	}
	if response.Code != 204 {
		t.Fatalf("status=%d", response.Code)
	}
	var state tunnelControl
	if _, err := b.control.read(t.Context(), brokerKey(credential.Token.TunnelUUID), &state); err != nil || !state.Active {
		t.Fatalf("valid connector not registered: %v", err)
	}
}
