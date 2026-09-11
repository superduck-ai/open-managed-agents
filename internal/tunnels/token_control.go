package tunnels

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type tunnelTokenDatabase interface {
	WithMCPTunnelTokenTx(context.Context, string, string, string, func(*db.MCPTunnelTokenTx) error) error
}

// Recovery and mutations hold the same DB row lock while changing NATS control.
// A crashed transaction releases its lock, allowing recovery from committed DB state.
func reconcileTunnelToken(ctx context.Context, database tunnelTokenDatabase, broker *Broker, scope tunnelScope, tunnelID string, expectedVersion int64) error {
	if broker == nil {
		return nil
	}
	return database.WithMCPTunnelTokenTx(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, tunnelID, func(tx *db.MCPTunnelTokenTx) error {
		if expectedVersion > 0 && tx.Token.Version != expectedVersion {
			return db.ErrInvalidState
		}
		return broker.ActivateTokenVersion(ctx, tx.Tunnel.UUID, tx.Token.Version)
	})
}
