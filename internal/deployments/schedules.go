package deployments

import (
	"context"
	"errors"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/yourbatis"
)

func (s *Store) writeDeploymentScheduleTx(ctx context.Context, tx *yourbatis.Tx, deployment db.Deployment) error {
	if deployment.ArchivedAt != nil || deployment.Status != "active" || len(deployment.Schedule) == 0 {
		return s.deleteDeploymentScheduleTx(ctx, tx, deployment.ExternalID)
	}
	opts, err := deploymentjobs.UpsertOpts(deployment.WorkspaceUUID, deployment.ExternalID, deployment.Schedule)
	if err != nil {
		return err
	}
	_, err = s.client.DurablePeriodicJobUpsertTx(ctx, tx.SQLTx(), opts)
	return err
}

func (s *Store) deleteDeploymentScheduleTx(ctx context.Context, tx *yourbatis.Tx, externalID string) error {
	_, err := s.client.DurablePeriodicJobDeleteTx(ctx, tx.SQLTx(), externalID)
	if errors.Is(err, rivertype.ErrNotFound) {
		return nil
	}
	return err
}
