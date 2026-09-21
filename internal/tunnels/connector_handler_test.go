package tunnels

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

type connectorMetadataDatabase struct {
	context       db.MCPTunnelTokenContext
	tunnel        db.MCPTunnel
	expectedToken string
	getError      error
}

type changingConnectorDatabase struct {
	connectorMetadataDatabase
	lookups int
}

func (d *changingConnectorDatabase) FindMCPTunnelTokenContext(ctx context.Context, id string, hash []byte) (db.MCPTunnelTokenContext, error) {
	d.lookups++
	credential, err := d.connectorMetadataDatabase.FindMCPTunnelTokenContext(ctx, id, hash)
	if d.lookups > 1 {
		now := time.Now()
		credential.TunnelArchivedAt = &now
	}
	return credential, err
}

func TestConnectorPollAuthorizesOnlyOnce(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	credential.TunnelUUID = "tunnel"
	command := testQueuedCommand("single-auth")
	if err := b.Enqueue(t.Context(), "tunnel", credential.TunnelExternalID, command); err != nil {
		t.Fatal(err)
	}
	database := &changingConnectorDatabase{connectorMetadataDatabase: connectorMetadataDatabase{context: credential}}
	h := &ConnectorHandler{cfg: b.cfg, broker: b, db: database}
	w := httptest.NewRecorder()
	if err := h.poll(w, connectorMetadataRequest("valid-token")); err != nil {
		t.Fatal(err)
	}
	if database.lookups != 1 || w.Code != 200 || !strings.Contains(w.Body.String(), command.RequestID) {
		t.Fatalf("poll lookups=%d status=%d", database.lookups, w.Code)
	}
}

func TestConnectorResponseBodyLimitExcludesWireEnvelope(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	credential.TunnelUUID = "tunnel"
	h := &ConnectorHandler{cfg: b.cfg, broker: b, db: connectorMetadataDatabase{context: credential}}

	id := "req_0123456789abcdefghijklmn"
	if err := b.Enqueue(t.Context(), "tunnel", credential.TunnelExternalID, testQueuedCommand(id)); err != nil {
		t.Fatal(err)
	}
	claim := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)[0]
	for _, extra := range []int{1, 0} {
		response := testTerminalResponse(id)
		response.JSONResponse = json.RawMessage(`"` + strings.Repeat("x", int(b.cfg.MaxBodyBytes)-2+extra) + `"`)
		response.ResponseHeaders = http.Header{"Content-Type": {"application/json"}}
		wire, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		r := connectorMetadataRequest("valid-token")
		r.Method = http.MethodPost
		r.Body = io.NopCloser(bytes.NewReader(wire))
		r.Header.Set(shardTokenHeader, claim.RequestID)
		r.Header.Set(connectorInstanceHeader, "a")
		err = h.postResponse(httptest.NewRecorder(), r)
		if extra > 0 && err == nil {
			t.Fatal("accepted oversized MCP body")
		}
		if extra == 0 && err != nil {
			t.Fatalf("valid maximum body rejected due to envelope overhead: %v", err)
		}
	}
}

func TestConnectorPollOptionsCapsHugeTimeoutWithoutDurationOverflow(t *testing.T) {
	t.Parallel()
	handler := &ConnectorHandler{cfg: config.TunnelConfig{PollTimeout: 30 * time.Second}}
	request := httptest.NewRequest(http.MethodGet, "/poll?timeout_ms=9223372036854775807", nil)
	_, timeout, err := handler.pollOptions(request)
	if err != nil {
		t.Fatalf("pollOptions: %v", err)
	}
	if timeout != 30*time.Second {
		t.Fatalf("poll timeout = %s, want 30s", timeout)
	}
}

func TestConnectorInstanceIDValidatesDisplayIdentifier(t *testing.T) {
	t.Parallel()

	missing := httptest.NewRequest(http.MethodGet, "/poll", nil)
	if got, err := connectorInstanceID(missing); err != nil || got != "legacy" {
		t.Fatalf("connectorInstanceID() stateless fallback = %q, %v, want legacy", got, err)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/poll", nil)
	invalid.Header.Set(connectorInstanceHeader, " instance-a ")
	if _, err := connectorInstanceID(invalid); err == nil {
		t.Fatal("connectorInstanceID() accepted surrounding whitespace")
	}

	valid := httptest.NewRequest(http.MethodGet, "/poll", nil)
	valid.Header.Set(connectorInstanceHeader, "instance-a")
	if got, err := connectorInstanceID(valid); err != nil || got != "instance-a" {
		t.Fatalf("connectorInstanceID() = %q, %v, want instance-a", got, err)
	}
}

func (d connectorMetadataDatabase) FindMCPTunnelTokenContext(_ context.Context, _ string, tokenHash []byte) (db.MCPTunnelTokenContext, error) {
	if d.expectedToken != "" {
		expectedHash := sha256.Sum256([]byte(d.expectedToken))
		if !bytes.Equal(tokenHash, expectedHash[:]) {
			return db.MCPTunnelTokenContext{}, db.ErrNotFound
		}
	}
	return d.context, nil
}

func (d connectorMetadataDatabase) GetMCPTunnel(context.Context, string, string, string) (db.MCPTunnel, error) {
	return d.tunnel, d.getError
}

func TestConnectorMetadataDoesNotHideDatabaseFailuresAsBadCredentials(t *testing.T) {
	t.Parallel()
	handler := &ConnectorHandler{db: connectorMetadataDatabase{
		context: activeConnectorContext(), getError: errors.New("database unavailable"),
	}}
	err := handler.metadata(httptest.NewRecorder(), connectorMetadataRequest("valid-token"))
	appError, ok := err.(*apperr.Error)
	if !ok || appError.Kind != apperr.Unavailable {
		t.Fatalf("metadata error = %#v, want unavailable", err)
	}
}

func TestConnectorMetadataUsesDisplayNameAndFallback(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name        string
		displayName *string
		wantName    string
	}{
		{name: "display name", displayName: stringPointer("Private tools"), wantName: "Private tools"},
		{name: "tunnel id fallback", wantName: "tunnel_0123456789abcdef0123456789abcdef"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			handler := &ConnectorHandler{db: connectorMetadataDatabase{
				context:       activeConnectorContext(),
				expectedToken: "valid-token",
				tunnel: db.MCPTunnel{
					ExternalID: "tunnel_0123456789abcdef0123456789abcdef", DisplayName: testCase.displayName,
				},
			}}
			response := httptest.NewRecorder()
			request := connectorMetadataRequest("valid-token")
			if err := handler.metadata(response, request); err != nil {
				t.Fatalf("metadata: %v", err)
			}
			var payload connectorTunnelMetadata
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatalf("decode metadata: %v", err)
			}
			if payload.ID != "tunnel_0123456789abcdef0123456789abcdef" || payload.Name != testCase.wantName || payload.Description != "" {
				t.Fatalf("metadata = %+v", payload)
			}
		})
	}
}

func TestConnectorMetadataRejectsMissingAndIncorrectCredentials(t *testing.T) {
	t.Parallel()
	handler := &ConnectorHandler{db: connectorMetadataDatabase{
		context: activeConnectorContext(), expectedToken: "valid-token",
	}}
	for _, testCase := range []struct {
		name  string
		token string
	}{
		{name: "missing token"},
		{name: "incorrect token", token: "incorrect-token"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := handler.metadata(httptest.NewRecorder(), connectorMetadataRequest(testCase.token))
			appError, ok := err.(*apperr.Error)
			if !ok || appError.Kind != apperr.Unauthenticated {
				t.Fatalf("metadata error = %#v, want unauthenticated", err)
			}
		})
	}
}

func TestConnectorMetadataRejectsRetiredAndArchivedCredentials(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, testCase := range []struct {
		name    string
		context db.MCPTunnelTokenContext
	}{
		{name: "retired token", context: func() db.MCPTunnelTokenContext {
			value := activeConnectorContext()
			value.RetiredAt = &now
			return value
		}()},
		{name: "archived tunnel", context: func() db.MCPTunnelTokenContext {
			value := activeConnectorContext()
			value.TunnelArchivedAt = &now
			return value
		}()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			handler := &ConnectorHandler{db: connectorMetadataDatabase{context: testCase.context}}
			err := handler.metadata(httptest.NewRecorder(), connectorMetadataRequest("valid-token"))
			appError, ok := err.(*apperr.Error)
			if !ok || appError.Kind != apperr.Unauthenticated {
				t.Fatalf("metadata error = %#v, want unauthenticated", err)
			}
		})
	}
}

func activeConnectorContext() db.MCPTunnelTokenContext {
	return db.MCPTunnelTokenContext{
		TunnelUUID:       "11111111-1111-4111-8111-111111111111",
		TunnelExternalID: "tunnel_0123456789abcdef0123456789abcdef",
		OrganizationUUID: "22222222-2222-4222-8222-222222222222",
		WorkspaceUUID:    "33333333-3333-4333-8333-333333333333",
	}
}

func connectorMetadataRequest(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/v1/tunnels/tunnel_0123456789abcdef0123456789abcdef", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("tunnel_id", "tunnel_0123456789abcdef0123456789abcdef")
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func stringPointer(value string) *string {
	return &value
}
