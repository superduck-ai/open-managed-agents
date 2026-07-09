package skillprewarm

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type SnapshotEnqueuer interface {
	EnqueueSnapshot(ctx context.Context, workspaceID int64, snapshot json.RawMessage, source string, sourceID string, trigger string) error
}

type FanoutEnqueuer interface {
	EnqueueFanout(ctx context.Context, workspaceID int64, skillID string, version string) error
}

type enqueueStore interface {
	EnqueueSkillPrewarmSnapshotJob(ctx context.Context, input db.SkillPrewarmSnapshotJobInput) error
	EnqueueSkillPrewarmFanoutJob(ctx context.Context, input db.SkillPrewarmFanoutJobInput) error
}

type Enqueuer struct {
	store enqueueStore
}

func NewEnqueuer(database *db.DB) *Enqueuer {
	if database == nil {
		return &Enqueuer{}
	}
	return &Enqueuer{store: database}
}

func (e *Enqueuer) EnqueueSnapshot(ctx context.Context, workspaceID int64, snapshot json.RawMessage, source string, sourceID string, trigger string) error {
	if e == nil || e.store == nil || !agentsnapshot.SnapshotHasSkills(snapshot) {
		return nil
	}
	return e.store.EnqueueSkillPrewarmSnapshotJob(ctx, db.SkillPrewarmSnapshotJobInput{
		WorkspaceID:   workspaceID,
		AgentSnapshot: snapshot,
		Source:        source,
		SourceID:      sourceID,
		Trigger:       trigger,
	})
}

func (e *Enqueuer) EnqueueFanout(ctx context.Context, workspaceID int64, skillID string, version string) error {
	if e == nil || e.store == nil || skillID == "" || version == "" {
		return nil
	}
	return e.store.EnqueueSkillPrewarmFanoutJob(ctx, db.SkillPrewarmFanoutJobInput{
		WorkspaceID: workspaceID,
		SkillID:     skillID,
		Version:     version,
	})
}
