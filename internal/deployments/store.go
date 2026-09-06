package deployments

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// Store coordinates deployment writes and durable schedules in one transaction.
// Configure must succeed before HTTP handlers or workers use it.
type Store struct {
	database *db.DB
	client   *river.Client[*sql.Tx]
}

func NewStore(database *db.DB) *Store {
	return &Store{database: database}
}

// Configure binds the shared River client and registers missing schedules before startup.
// It must not run concurrently with handlers or workers. A failed configuration
// leaves the store unavailable for writes and may be retried before startup.
func (s *Store) Configure(ctx context.Context, client *river.Client[*sql.Tx]) error {
	s.client = client
	if client == nil {
		return errStoreNotConfigured
	}
	if err := s.registerMissingSchedules(ctx); err != nil {
		s.client = nil
		return err
	}
	return nil
}

func (s *Store) transaction(ctx context.Context, fn func(*db.Tx) error) error {
	if s.client == nil {
		return errStoreNotConfigured
	}
	return s.database.Transaction(ctx, fn)
}

func (s *Store) Create(ctx context.Context, deployment db.Deployment) (db.Deployment, error) {
	var created db.Deployment
	err := s.transaction(ctx, func(tx *db.Tx) error {
		var err error
		created, err = tx.CreateDeployment(ctx, deployment)
		if err != nil || len(created.Schedule) == 0 {
			return err
		}
		return s.writeDeploymentScheduleTx(ctx, tx, created)
	})
	return created, err
}

func (s *Store) Update(ctx context.Context, workspaceUUID, externalID string, input db.UpdateDeploymentInput) (db.Deployment, error) {
	var updated db.Deployment
	err := s.transaction(ctx, func(tx *db.Tx) error {
		var scheduleChanged bool
		var err error
		updated, scheduleChanged, err = tx.UpdateDeployment(ctx, workspaceUUID, externalID, input)
		if err != nil || !scheduleChanged {
			return err
		}
		return s.writeDeploymentScheduleTx(ctx, tx, updated)
	})
	return updated, err
}

func (s *Store) Pause(ctx context.Context, workspaceUUID, externalID string, pausedReason json.RawMessage) (db.Deployment, error) {
	var paused db.Deployment
	err := s.transaction(ctx, func(tx *db.Tx) error {
		var err error
		paused, err = tx.PauseDeployment(ctx, workspaceUUID, externalID, pausedReason)
		if err != nil {
			return err
		}
		return s.deleteDeploymentScheduleTx(ctx, tx, paused.ExternalID)
	})
	return paused, err
}

func (s *Store) Unpause(ctx context.Context, workspaceUUID, externalID string) (db.Deployment, error) {
	var unpaused db.Deployment
	err := s.transaction(ctx, func(tx *db.Tx) error {
		var err error
		unpaused, err = tx.UnpauseDeployment(ctx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		return s.writeDeploymentScheduleTx(ctx, tx, unpaused)
	})
	return unpaused, err
}

func (s *Store) Archive(ctx context.Context, workspaceUUID, externalID string) (db.Deployment, error) {
	var archived db.Deployment
	err := s.transaction(ctx, func(tx *db.Tx) error {
		var err error
		archived, err = tx.ArchiveDeployment(ctx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		return s.deleteDeploymentScheduleTx(ctx, tx, archived.ExternalID)
	})
	return archived, err
}

func (s *Store) ApplyScheduledOccurrence(ctx context.Context, input db.ApplyScheduledOccurrenceInput) error {
	return s.transaction(ctx, func(tx *db.Tx) error {
		if err := tx.ApplyScheduledOccurrence(ctx, input); err != nil {
			return err
		}
		if input.ArchiveDeployment || len(input.AutoPauseReason) > 0 {
			return s.deleteDeploymentScheduleTx(ctx, tx, input.Deployment.ExternalID)
		}
		return nil
	})
}

// ArchiveAgent commits the root agent, its deployments, and schedule deletions together.
func (s *Store) ArchiveAgent(ctx context.Context, workspaceUUID, externalID string) (db.Agent, error) {
	var archived db.Agent
	err := s.transaction(ctx, func(tx *db.Tx) error {
		var err error
		archived, err = tx.ArchiveAgent(ctx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		deployments, err := tx.ArchiveDeploymentsByRootAgent(ctx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		for _, deployment := range deployments {
			if len(deployment.Schedule) == 0 {
				continue
			}
			if err := s.deleteDeploymentScheduleTx(ctx, tx, deployment.ExternalID); err != nil {
				return err
			}
		}
		return nil
	})
	return archived, err
}
