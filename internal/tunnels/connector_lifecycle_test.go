package tunnels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type lifecycleConnectorDatabase struct {
	connectorMetadataDatabase
	revoked atomic.Bool
	lookups atomic.Int32
	entered chan struct{}
	archive bool
	failure error
}

func (d *lifecycleConnectorDatabase) FindMCPTunnelTokenContext(ctx context.Context, id string, hash []byte) (db.MCPTunnelTokenContext, error) {
	credential, err := d.connectorMetadataDatabase.FindMCPTunnelTokenContext(ctx, id, hash)
	if d.revoked.Load() {
		now := time.Now()
		if d.archive {
			credential.TunnelArchivedAt = &now
		} else {
			credential.RetiredAt = &now
		}
	}
	if d.lookups.Add(1) == 1 && d.entered != nil {
		close(d.entered)
	}
	if d.failure != nil {
		return db.MCPTunnelTokenContext{}, d.failure
	}
	return credential, err
}

func TestConnectorPollRejectsInactiveCredentials(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, token       string
		retired, archived *time.Time
		failure           error
		kind              apperr.Kind
	}{
		{name: "missing", kind: apperr.Unauthenticated},
		{name: "wrong", token: "wrong", kind: apperr.Unauthenticated},
		{name: "retired", token: "valid-token", retired: &now, kind: apperr.Unauthenticated},
		{name: "archived", token: "valid-token", archived: &now, kind: apperr.Unauthenticated},
		{name: "database unavailable", token: "valid-token", failure: errors.New("offline"), kind: apperr.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credential := activeConnectorContext()
			credential.RetiredAt = tc.retired
			credential.TunnelArchivedAt = tc.archived
			database := &lifecycleConnectorDatabase{connectorMetadataDatabase: connectorMetadataDatabase{context: credential, expectedToken: "valid-token"}, failure: tc.failure}
			h := &ConnectorHandler{db: database}
			assertTunnelErrorKind(t, h.poll(httptest.NewRecorder(), connectorMetadataRequest(tc.token)), tc.kind)
		})
	}
}

func TestConnectorAuthorizedLongPollSurvivesRevocation(t *testing.T) {
	for _, archive := range []bool{false, true} {
		t.Run(map[bool]string{false: "rotation", true: "archive"}[archive], func(t *testing.T) {
			b := testNATSBroker(t, brokerTestConfig())
			credential := activeConnectorContext()
			credential.TunnelUUID = "tunnel"
			database := &lifecycleConnectorDatabase{connectorMetadataDatabase: connectorMetadataDatabase{context: credential, expectedToken: "valid-token"}, entered: make(chan struct{}), archive: archive}
			h := &ConnectorHandler{cfg: b.cfg, db: database, broker: b}
			w := httptest.NewRecorder()
			done := make(chan error, 1)
			go func() { done <- h.poll(w, connectorMetadataRequest("valid-token")) }()
			<-database.entered
			database.revoked.Store(true)
			command := testQueuedCommand("req_0123456789abcdefghijklmn")
			if err := b.Enqueue(t.Context(), "tunnel", credential.TunnelExternalID, command); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("authorized poll did not finish")
			}
			if database.lookups.Load() != 1 || w.Code != 200 {
				t.Fatalf("lookups=%d status=%d", database.lookups.Load(), w.Code)
			}
			assertTunnelErrorKind(t, h.poll(httptest.NewRecorder(), connectorMetadataRequest("valid-token")), apperr.Unauthenticated)
			// Response must complete even when the database is entirely absent.
			h.db = nil
			response := testTerminalResponse(command.RequestID)
			if err := h.postResponse(httptest.NewRecorder(), boundResponseRequest(t, "valid-token", credential.TunnelExternalID, response, command.RequestID)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConnectorResponseUsesOnlyRequestBinding(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	tunnelID := activeConnectorContext().TunnelExternalID
	id := "req_0123456789abcdefghijklmn"
	if err := b.Enqueue(t.Context(), "tunnel", tunnelID, testQueuedCommand(id)); err != nil {
		t.Fatal(err)
	}
	h := &ConnectorHandler{cfg: b.cfg, broker: b} // A DB lookup would panic.
	response := testTerminalResponse(id)
	assertTunnelErrorKind(t, h.postResponse(httptest.NewRecorder(), boundResponseRequest(t, "valid-token", tunnelID, response, id)), apperr.NotFound)
	pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
	for _, tc := range []struct {
		name, token, tunnel, shard string
		kind                       apperr.Kind
	}{
		{"missing token", "", tunnelID, id, apperr.Unauthenticated},
		{"rotated token", "new-token", tunnelID, id, apperr.NotFound},
		{"wrong tunnel", "valid-token", "tunnel_other", id, apperr.NotFound},
		{"wrong shard", "valid-token", tunnelID, "wrong", apperr.NotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertTunnelErrorKind(t, h.postResponse(httptest.NewRecorder(), boundResponseRequest(t, tc.token, tc.tunnel, response, tc.shard)), tc.kind)
		})
	}
	for i := 0; i < 2; i++ {
		if i == 1 {
			b.now = func() time.Time { return time.Now().Add(time.Minute) }
		}
		if err := h.postResponse(httptest.NewRecorder(), boundResponseRequest(t, "valid-token", tunnelID, response, id)); err != nil {
			t.Fatal(err)
		}
		response.JSONResponse = json.RawMessage(`{"result":"must not overwrite"}`)
	}
	record, _, err := b.readRequest(t.Context(), "tunnel", id)
	if err != nil || bytes.Equal(record.Response.JSONResponse, response.JSONResponse) {
		t.Fatalf("duplicate replaced terminal result: %v", err)
	}
}

func boundResponseRequest(t *testing.T, token, tunnelID string, response TunnelResponse, shard string) *http.Request {
	t.Helper()
	wire, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/response", io.NopCloser(bytes.NewReader(wire)))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(shardTokenHeader, shard)
	r.Header.Set(connectorInstanceHeader, "different-instance")
	route := chi.NewRouteContext()
	route.URLParams.Add("tunnel_id", tunnelID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
}
func assertTunnelErrorKind(t *testing.T, err error, kind apperr.Kind) {
	t.Helper()
	var app *apperr.Error
	if !errors.As(err, &app) || app.Kind != kind {
		t.Fatalf("error=%v, want %v", err, kind)
	}
}

func TestConnectorDeclarationsShareConsumer(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	credential.TunnelUUID = "tunnel"
	h := &ConnectorHandler{cfg: b.cfg, broker: b, db: connectorMetadataDatabase{context: credential}}
	for _, declaration := range []string{`{"version":1,"channels":[{"name":"main","proc_affinity":true}]}`, `{"version":2,"channels":[{"name":"main","proc_affinity":false,"stateless":true}]}`} {
		r := connectorMetadataRequest("valid-token")
		r.URL.RawQuery = "timeout_ms=0"
		r.Header.Set(serverInfoHeader, declaration)
		if err := h.poll(httptest.NewRecorder(), r); err != nil {
			t.Fatal(err)
		}
	}
	info, err := b.commands.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Consumers != 1 {
		t.Fatalf("declarations created %d consumers", info.State.Consumers)
	}
	if _, err := b.js.KeyValue(t.Context(), "OMA_TUNNEL_CONTROL_V1"); err == nil {
		t.Fatal("Control KV was created")
	}
}

func TestConnectorRejectsCanceledAndExpiredResponses(t *testing.T) {
	for _, canceled := range []bool{true, false} {
		t.Run(map[bool]string{true: "canceled", false: "expired"}[canceled], func(t *testing.T) {
			b := testNATSBroker(t, brokerTestConfig())
			id := "req_0123456789abcdefghijklmn"
			tunnelID := activeConnectorContext().TunnelExternalID
			command := testQueuedCommand(id)
			if err := b.Enqueue(t.Context(), "tunnel", tunnelID, command); err != nil {
				t.Fatal(err)
			}
			pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
			if canceled {
				if err := b.Cancel(t.Context(), "tunnel", id); err != nil {
					t.Fatal(err)
				}
			} else {
				b.now = func() time.Time { return command.ExpiresAt.Add(time.Second) }
			}
			h := &ConnectorHandler{cfg: b.cfg, broker: b}
			assertTunnelErrorKind(t, h.postResponse(httptest.NewRecorder(), boundResponseRequest(t, "valid-token", tunnelID, testTerminalResponse(id), id)), apperr.NotFound)
		})
	}
}

func TestIngressWithoutConnectorWaitsUntilDeadline(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.RequestTimeout = time.Second
	b := testNATSBroker(t, cfg)
	h := &IngressHandler{cfg: cfg, broker: b}
	r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	started := time.Now()
	err := h.forwardTunnel(httptest.NewRecorder(), r, db.MCPTunnel{UUID: "tunnel", ExternalID: "tunnel"}, "main", CommandTypeJSONRPC)
	assertTunnelErrorKind(t, err, apperr.Timeout)
	if time.Since(started) < cfg.RequestTimeout {
		t.Fatal("offline request failed before its deadline")
	}
}
