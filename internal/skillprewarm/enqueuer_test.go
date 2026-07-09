package skillprewarm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestEnqueuerSkipsSnapshotsWithoutSkills(t *testing.T) {
	store := &fakeEnqueueStore{}
	enqueuer := &Enqueuer{store: store}

	if err := enqueuer.EnqueueSnapshot(context.Background(), 1, json.RawMessage(`{"skills":[]}`), "agent", "agent_1", "agent_create"); err != nil {
		t.Fatalf("EnqueueSnapshot error = %v", err)
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("snapshot jobs = %d, want 0", len(store.snapshots))
	}
}

func TestEnqueuerEnqueuesSnapshotWithSkills(t *testing.T) {
	store := &fakeEnqueueStore{}
	enqueuer := &Enqueuer{store: store}

	snapshot := json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_1","version":"latest"}]}`)
	if err := enqueuer.EnqueueSnapshot(context.Background(), 7, snapshot, "agent", "agent_1", "agent_create"); err != nil {
		t.Fatalf("EnqueueSnapshot error = %v", err)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("snapshot jobs = %d, want 1", len(store.snapshots))
	}
	got := store.snapshots[0]
	if got.WorkspaceID != 7 || got.Source != "agent" || got.SourceID != "agent_1" || got.Trigger != "agent_create" {
		t.Fatalf("snapshot input = %+v", got)
	}
}

func TestEnqueuerEnqueuesFanout(t *testing.T) {
	store := &fakeEnqueueStore{}
	enqueuer := &Enqueuer{store: store}

	if err := enqueuer.EnqueueFanout(context.Background(), 3, "skill_1", "20260708"); err != nil {
		t.Fatalf("EnqueueFanout error = %v", err)
	}
	if len(store.fanouts) != 1 {
		t.Fatalf("fanout jobs = %d, want 1", len(store.fanouts))
	}
	got := store.fanouts[0]
	if got.WorkspaceID != 3 || got.SkillID != "skill_1" || got.Version != "20260708" {
		t.Fatalf("fanout input = %+v", got)
	}
}

type fakeEnqueueStore struct {
	snapshots []db.SkillPrewarmSnapshotJobInput
	fanouts   []db.SkillPrewarmFanoutJobInput
}

func (s *fakeEnqueueStore) EnqueueSkillPrewarmSnapshotJob(_ context.Context, input db.SkillPrewarmSnapshotJobInput) error {
	s.snapshots = append(s.snapshots, input)
	return nil
}

func (s *fakeEnqueueStore) EnqueueSkillPrewarmFanoutJob(_ context.Context, input db.SkillPrewarmFanoutJobInput) error {
	s.fanouts = append(s.fanouts, input)
	return nil
}
