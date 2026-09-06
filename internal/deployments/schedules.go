package deployments

import (
	"context"
	"errors"
	"fmt"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
)

func (s *Store) writeDeploymentScheduleTx(ctx context.Context, tx *db.Tx, deployment db.Deployment) error {
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

func (s *Store) deleteDeploymentScheduleTx(ctx context.Context, tx *db.Tx, externalID string) error {
	_, err := s.client.DurablePeriodicJobDeleteTx(ctx, tx.SQLTx(), externalID)
	if errors.Is(err, rivertype.ErrNotFound) {
		return nil
	}
	return err
}

// registerMissingSchedules imports schedules created before durable scheduling.
// Existing River records are authoritative for execution and are left untouched.
func (s *Store) registerMissingSchedules(ctx context.Context) error {
	states, err := s.database.ListActiveDeploymentSchedules(ctx)
	if err != nil {
		return err
	}
	for _, state := range states {
		err := s.transaction(ctx, func(tx *db.Tx) error {
			deployment, err := tx.LockDeployment(ctx, state.WorkspaceUUID, state.ExternalID)
			if errors.Is(err, db.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if deployment.ArchivedAt != nil || deployment.Status != "active" || len(deployment.Schedule) == 0 {
				return nil
			}
			_, err = s.client.DurablePeriodicJobGetTx(ctx, tx.SQLTx(), deployment.ExternalID)
			if err == nil {
				return nil
			}
			if !errors.Is(err, rivertype.ErrNotFound) {
				return err
			}
			return s.writeDeploymentScheduleTx(ctx, tx, deployment)
		})
		if err != nil {
			return fmt.Errorf("register deployment %s: %w", state.ExternalID, err)
		}
	}
	return nil
}
