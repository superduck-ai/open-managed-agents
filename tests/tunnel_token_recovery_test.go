//go:build e2e

package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

func TestTunnelTokenRecoverySerializesWithArchiveAndRotation(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = managedTunnelDatabase(t, cfg.Database.URL)
	app := newTestAppWithStore(t, &cfg, newFakeStore("tunnel-token-recovery"))
	t.Cleanup(app.close)
	connection := managedTunnelNATS(t)
	broker, err := tunnels.NewBroker(t.Context(), connection, cfg.Tunnel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	created, err := client.Beta.Tunnels.New(t.Context(), anthropic.BetaTunnelNewParams{})
	if err != nil {
		t.Fatal(err)
	}
	revealed, err := client.Beta.Tunnels.RevealToken(t.Context(), created.ID, anthropic.BetaTunnelRevealTokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	ids := getDefaultDBIDs(t, app.pool)
	record, err := app.db.GetMCPTunnel(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := tunnels.NewConnectorHandler(cfg.Tunnel, app.db, broker, nil)
	poll := func(ctx context.Context, token string) int {
		request := httptest.NewRequest("GET", "/v1/tunnels/"+created.ID+"/poll?timeout_ms=0", nil).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	rollback := errors.New("simulate interrupted transaction")
	err = app.db.WithMCPTunnelTokenTx(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID, func(tx *db.MCPTunnelTokenTx) error {
		if err := broker.SuspendTokenVersion(t.Context(), record.UUID, tx.Token.Version); err != nil {
			return err
		}
		// A recovery must wait for this transaction, not reactivate its suspended token.
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		defer cancel()
		if status := poll(ctx, revealed.TunnelToken); status != http.StatusServiceUnavailable {
			t.Fatal("recovery bypassed the row lock")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	// A recreated OMA broker still sees the persisted suspension, then Poll repairs it.
	fresh, err := tunnels.NewBroker(t.Context(), connection, cfg.Tunnel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fresh.Close)
	handler = tunnels.NewConnectorHandler(cfg.Tunnel, app.db, fresh, nil)
	if status := poll(t.Context(), revealed.TunnelToken); status != http.StatusNoContent {
		t.Fatalf("rollback recovery status=%d", status)
	}
	rotated, err := client.Beta.Tunnels.RotateToken(t.Context(), created.ID, anthropic.BetaTunnelRotateTokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.SuspendTokenVersion(t.Context(), record.UUID, 2); err != nil {
		t.Fatal(err)
	}
	if status := poll(t.Context(), revealed.TunnelToken); status != 401 {
		t.Fatalf("old token status=%d", status)
	}
	if status := poll(t.Context(), rotated.TunnelToken); status != http.StatusNoContent {
		t.Fatalf("committed rotation recovery status=%d", status)
	}
	// Commit archive after suspension; subsequent recovery cannot restore that token.
	err = app.db.WithMCPTunnelTokenTx(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID, func(tx *db.MCPTunnelTokenTx) error {
		if err := fresh.SuspendTokenVersion(t.Context(), record.UUID, tx.Token.Version); err != nil {
			return err
		}
		_, err := tx.Archive(t.Context())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if status := poll(t.Context(), rotated.TunnelToken); status != 401 {
		t.Fatalf("archived token status=%d", status)
	}
}
