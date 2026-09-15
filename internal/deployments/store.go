package deployments

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/yourbatis"
)

// Store coordinates deployment writes and durable schedules in one transaction.
// Configure must be called before HTTP handlers or workers use it.
type Store struct {
	eventPayloads *eventpayload.Store
	database      *db.DB
	client        *river.Client[*sql.Tx]
}

func NewStore(database *db.DB) *Store {
	return &Store{database: database, eventPayloads: eventpayload.New(database, nil)}
}

// Configure binds the shared River client before HTTP handlers or workers start.
func (s *Store) Configure(client *river.Client[*sql.Tx]) {
	s.client = client
}

func (s *Store) transaction(ctx context.Context, fn func(*yourbatis.Tx) error) error {
	if s.client == nil {
		return errStoreNotConfigured
	}
	return s.database.DeploymentTransaction(ctx, fn)
}

func (s *Store) Create(ctx context.Context, deployment db.Deployment) (db.Deployment, error) {
	var created db.Deployment
	err := s.transaction(ctx, func(tx *yourbatis.Tx) error {
		var err error
		created, err = s.database.CreateDeploymentTx(ctx, tx, deployment)
		if err != nil || len(created.Schedule) == 0 {
			return err
		}
		return s.writeDeploymentScheduleTx(ctx, tx, created)
	})
	return created, err
}

func (s *Store) Update(ctx context.Context, workspaceUUID, externalID string, input db.UpdateDeploymentInput) (db.Deployment, error) {
	var updated db.Deployment
	err := s.transaction(ctx, func(tx *yourbatis.Tx) error {
		var scheduleChanged bool
		var err error
		updated, scheduleChanged, err = s.database.UpdateDeploymentTx(ctx, tx, workspaceUUID, externalID, input)
		if err != nil || !scheduleChanged {
			return err
		}
		return s.writeDeploymentScheduleTx(ctx, tx, updated)
	})
	return updated, err
}

func (s *Store) Pause(ctx context.Context, workspaceUUID, externalID string, pausedReason json.RawMessage) (db.Deployment, error) {
	var paused db.Deployment
	err := s.transaction(ctx, func(tx *yourbatis.Tx) error {
		var err error
		paused, err = s.database.PauseDeploymentTx(ctx, tx, workspaceUUID, externalID, pausedReason)
		if err != nil {
			return err
		}
		return s.deleteDeploymentScheduleTx(ctx, tx, paused.ExternalID)
	})
	return paused, err
}

func (s *Store) Unpause(ctx context.Context, workspaceUUID, externalID string) (db.Deployment, error) {
	var unpaused db.Deployment
	err := s.transaction(ctx, func(tx *yourbatis.Tx) error {
		var resumed bool
		var err error
		unpaused, resumed, err = s.database.UnpauseDeploymentTx(ctx, tx, workspaceUUID, externalID)
		if err != nil || !resumed {
			return err
		}
		return s.writeDeploymentScheduleTx(ctx, tx, unpaused)
	})
	return unpaused, err
}

func (s *Store) Archive(ctx context.Context, workspaceUUID, externalID string) (db.Deployment, error) {
	var archived db.Deployment
	err := s.transaction(ctx, func(tx *yourbatis.Tx) error {
		var err error
		archived, err = s.database.ArchiveDeploymentTx(ctx, tx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		return s.deleteDeploymentScheduleTx(ctx, tx, archived.ExternalID)
	})
	return archived, err
}

func (s *Store) ApplyScheduledOccurrence(ctx context.Context, input db.ApplyScheduledOccurrenceInput) error {
	prepared, err := s.eventPayloads.PreparePublic(ctx, input.Deployment.OrganizationUUID, input.Deployment.WorkspaceUUID, input.Events)
	if err != nil {
		return err
	}
	input.Events = prepared
	return s.transaction(ctx, func(tx *yourbatis.Tx) error {
		if err := s.database.ApplyScheduledOccurrenceTx(ctx, tx, input); err != nil {
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
	err := s.transaction(ctx, func(tx *yourbatis.Tx) error {
		var err error
		archived, err = s.database.ArchiveAgentTx(ctx, tx, workspaceUUID, externalID)
		if err != nil {
			return err
		}
		deployments, err := s.database.ArchiveDeploymentsByRootAgentTx(ctx, tx, workspaceUUID, externalID)
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

func (s *Store) WithEventPayloadStorage(objects storage.ObjectStore) *Store {
	s.eventPayloads = eventpayload.New(s.database, objects)
	return s
}

func (s *Store) CreateManualRun(ctx context.Context, input db.CreateManualDeploymentRunInput) (db.DeploymentRun, db.Session, db.SessionThread, []db.SessionEvent, error) {
	original := input.Events
	prepared, err := s.eventPayloads.PreparePublic(ctx, input.Session.Session.OrganizationUUID, input.Session.Session.WorkspaceUUID, original)
	if err != nil {
		return db.DeploymentRun{}, db.Session{}, db.SessionThread{}, nil, err
	}
	input.Events = prepared
	run, session, thread, events, err := s.database.CreateManualDeploymentRun(ctx, input)
	if err != nil {
		return run, session, thread, nil, err
	}
	return run, session, thread, eventpayload.RestoreCreatedPublic(events, original), nil
}
