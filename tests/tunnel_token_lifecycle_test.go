package tests

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// The app has no Tunnel broker: management transactions must depend only on PG.
func TestGoSDKTunnelTokenLifecycleWithoutBroker(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("tunnel-token-lifecycle"))
	defer app.close()
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	created, err := client.Beta.Tunnels.New(t.Context(), anthropic.BetaTunnelNewParams{})
	if err != nil {
		t.Fatal(err)
	}
	revealed, err := client.Beta.Tunnels.RevealToken(t.Context(), created.ID, anthropic.BetaTunnelRevealTokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(revealed.TunnelToken))
	credential, err := app.db.FindMCPTunnelTokenContext(t.Context(), created.ID, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("abort archive transaction")
	err = app.db.WithMCPTunnelTokenTx(t.Context(), credential.OrganizationUUID, credential.WorkspaceUUID, created.ID, func(tx *db.MCPTunnelTokenTx) error {
		if _, err := tx.Archive(t.Context()); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("archive rollback failed: %v", err)
	}
	credential, err = app.db.FindMCPTunnelTokenContext(t.Context(), created.ID, hash[:])
	if err != nil || credential.TunnelArchivedAt != nil || credential.ArchivedAt != nil {
		t.Fatal("rolled back archive changed credential status")
	}
	rotated, err := client.Beta.Tunnels.RotateToken(t.Context(), created.ID, anthropic.BetaTunnelRotateTokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	old, err := app.db.FindMCPTunnelTokenContext(t.Context(), created.ID, hash[:])
	if err != nil || old.RetiredAt == nil {
		t.Fatal("rotation did not retire old credential")
	}
	hash = sha256.Sum256([]byte(rotated.TunnelToken))
	current, err := app.db.FindMCPTunnelTokenContext(t.Context(), created.ID, hash[:])
	if err != nil || current.RetiredAt != nil || current.TunnelArchivedAt != nil {
		t.Fatal("new credential is not active")
	}
	for range 2 {
		if _, err := client.Beta.Tunnels.Archive(t.Context(), created.ID, anthropic.BetaTunnelArchiveParams{}); err != nil {
			t.Fatal(err)
		}
	}
	current, err = app.db.FindMCPTunnelTokenContext(t.Context(), created.ID, hash[:])
	if err != nil || current.ArchivedAt == nil || current.TunnelArchivedAt == nil {
		t.Fatal("archive transaction did not update resource and credential")
	}
}
