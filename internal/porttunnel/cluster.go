package porttunnel

import (
	"context"
	"log/slog"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type ClusterRouter struct {
	local  *Registry
	owners OwnerStore
	client *ForwardClient
	logger *slog.Logger
}

func NewClusterRouter(local *Registry, owners OwnerStore, client *ForwardClient, logger *slog.Logger) *ClusterRouter {
	return &ClusterRouter{local: local, owners: owners, client: client, logger: logging.LoggerOrDefault(logger)}
}

func (c *ClusterRouter) Lookup(ctx context.Context, codeSessionUUID string) (Tunnel, error) {
	if tunnel, err := c.local.Lookup(ctx, codeSessionUUID); err == nil {
		return tunnel, nil
	}
	owner, err := c.owner(ctx, codeSessionUUID)
	if err != nil {
		return nil, err
	}
	address, _, _ := strings.Cut(owner, "#")
	if owner == "" || owner == c.local.Owner() || address == "" {
		return nil, ErrNoTunnel
	}
	return &remoteTunnel{
		client:          c.client,
		owner:           owner,
		address:         address,
		codeSessionUUID: codeSessionUUID,
		release:         func(owner string) { c.release(codeSessionUUID, owner) },
		logger:          c.logger,
	}, nil
}

func (c *ClusterRouter) WaitOnline(ctx context.Context, codeSessionUUID string) error {
	return c.local.waitOnline(ctx, codeSessionUUID, func(ctx context.Context) bool {
		owner, err := c.owner(ctx, codeSessionUUID)
		return err == nil && owner != "" && owner != c.local.Owner()
	})
}

func (c *ClusterRouter) owner(ctx context.Context, codeSessionUUID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ownerOperationTimeout)
	defer cancel()
	return c.owners.Get(ctx, codeSessionUUID)
}

func (c *ClusterRouter) release(codeSessionUUID, owner string) {
	ctx, cancel := context.WithTimeout(context.Background(), ownerOperationTimeout)
	defer cancel()
	if err := c.owners.Release(ctx, codeSessionUUID, owner); err != nil {
		c.logger.WarnContext(ctx, "release stale session port tunnel owner", "code_session_uuid", codeSessionUUID, "owner", owner, "error", err)
	}
}
