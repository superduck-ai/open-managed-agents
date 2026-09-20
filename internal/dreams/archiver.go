package dreams

import (
	"context"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const internalSessionArchiveLimit = 20

type internalSessionArchiveStore interface {
	ListDreamsAwaitingInternalSessionArchive(ctx context.Context, limit int) ([]db.Dream, error)
	GetSession(ctx context.Context, workspaceUUID, externalID string) (db.Session, bool, error)
	ArchiveSession(ctx context.Context, workspaceUUID, externalID string) (db.Session, error)
}

// InternalSessionArchiver archives internal Dream Sessions after the Dream
// contract is already terminal. It does not write Dream status.
type InternalSessionArchiver struct {
	store internalSessionArchiveStore
}

func NewInternalSessionArchiver(database *db.DB) *InternalSessionArchiver {
	if database == nil {
		return &InternalSessionArchiver{}
	}
	return &InternalSessionArchiver{store: database}
}

func (a *InternalSessionArchiver) RunOnce(ctx context.Context) error {
	if a == nil || a.store == nil {
		return nil
	}
	dreams, err := a.store.ListDreamsAwaitingInternalSessionArchive(ctx, internalSessionArchiveLimit)
	if err != nil {
		return err
	}
	for _, dream := range dreams {
		if err := a.ArchiveIfStopped(ctx, dream); err != nil {
			return err
		}
	}
	return nil
}

func (a *InternalSessionArchiver) ArchiveIfStopped(ctx context.Context, dream db.Dream) error {
	if a == nil || a.store == nil {
		return nil
	}
	output, err := dreamOutput(dream.Outputs)
	if err != nil {
		return nil
	}
	session, found, err := a.store.GetSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
	if err != nil {
		return err
	}
	if !found || session.ArchivedAt != nil {
		return nil
	}
	if session.Status == "running" || session.Status == "rescheduling" {
		return nil
	}
	_, err = a.store.ArchiveSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
	if err == nil || errors.Is(err, db.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("archive internal Dream session: %w", err)
}
