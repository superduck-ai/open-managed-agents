package skillprewarm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestWorkerSnapshotPreparesMountAndCompletes(t *testing.T) {
	store := &fakeWorkerStore{
		jobs: []db.SkillPrewarmJob{{
			ID:          1,
			ExternalID:  "job_1",
			WorkspaceID: 42,
			Payload:     json.RawMessage(`{"kind":"snapshot","agent_snapshot":{"skills":[{"type":"custom","skill_id":"skill_1","version":"latest"}]}}`),
		}},
	}
	resolver := &fakeResolver{runtimeSkills: []skillsapi.RuntimeSkill{{Source: "custom", SkillID: "skill_1", Version: "1"}}}
	preparer := &fakePreparer{}
	worker := NewWorker(store, store, store, resolver, preparer)

	if err := worker.RunOnce(context.Background(), "worker_1"); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if len(store.completed) != 1 || store.completed[0] != 1 {
		t.Fatalf("completed = %+v, want [1]", store.completed)
	}
	if len(store.failed) != 0 {
		t.Fatalf("failed = %+v, want none", store.failed)
	}
	if resolver.workspaceID != 42 {
		t.Fatalf("resolver workspaceID = %d, want 42", resolver.workspaceID)
	}
	if len(preparer.received) != 1 || preparer.received[0].SkillID != "skill_1" {
		t.Fatalf("preparer received = %+v", preparer.received)
	}
}

func TestWorkerSnapshotFailureRetries(t *testing.T) {
	store := &fakeWorkerStore{
		jobs: []db.SkillPrewarmJob{{
			ID:          2,
			ExternalID:  "job_2",
			WorkspaceID: 42,
			Attempts:    1,
			Payload:     json.RawMessage(`{"kind":"snapshot","agent_snapshot":{"skills":[{"type":"custom","skill_id":"skill_1","version":"latest"}]}}`),
		}},
	}
	worker := NewWorker(store, store, store, &fakeResolver{err: errors.New("resolve failed")}, &fakePreparer{})

	if err := worker.RunOnce(context.Background(), "worker_1"); err == nil {
		t.Fatal("RunOnce error = nil, want failure")
	}
	if len(store.failed) != 1 {
		t.Fatalf("failed = %+v, want one failure", store.failed)
	}
	if store.failed[0].id != 2 || store.failed[0].attempts != 1 || store.failed[0].maxAttempts != maxAttempts {
		t.Fatalf("failure = %+v", store.failed[0])
	}
	if len(store.completed) != 0 {
		t.Fatalf("completed = %+v, want none", store.completed)
	}
}

func TestWorkerFanoutEnqueuesSnapshotsAndContinuation(t *testing.T) {
	store := &fakeWorkerStore{
		jobs: []db.SkillPrewarmJob{{
			ID:          3,
			ExternalID:  "job_3",
			WorkspaceID: 42,
			Payload:     json.RawMessage(`{"kind":"fanout","skill_id":"skill_1","version":"20260708","after_agent_id":5,"after_deployment_id":7}`),
		}},
		agents: []db.Agent{{
			ID:             10,
			ExternalID:     "agent_1",
			CurrentVersion: 2,
			Name:           "agent",
			Model:          json.RawMessage(`{}`),
			Skills:         json.RawMessage(`[{"type":"custom","skill_id":"skill_1","version":"latest"}]`),
		}},
		deployments: []db.Deployment{{
			ID:            20,
			ExternalID:    "dep_1",
			AgentSnapshot: json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_1","version":"latest"}]}`),
		}},
		hasMoreAgents: true,
	}
	worker := NewWorker(store, store, store, &fakeResolver{}, &fakePreparer{})

	if err := worker.RunOnce(context.Background(), "worker_1"); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if len(store.snapshotInputs) != 2 {
		t.Fatalf("snapshot inputs = %+v, want 2", store.snapshotInputs)
	}
	for _, input := range store.snapshotInputs {
		if input.Trigger != "skill_version_create" || input.TriggerSkillID != "skill_1" || input.TriggerSkillVersion != "20260708" {
			t.Fatalf("snapshot input = %+v", input)
		}
	}
	if store.snapshotInputs[0].Source != "agent" || store.snapshotInputs[0].SourceID != "agent_1" {
		t.Fatalf("agent snapshot input = %+v", store.snapshotInputs[0])
	}
	if store.snapshotInputs[1].Source != "deployment" || store.snapshotInputs[1].SourceID != "dep_1" {
		t.Fatalf("deployment snapshot input = %+v", store.snapshotInputs[1])
	}
	if len(store.fanoutInputs) != 1 {
		t.Fatalf("fanout inputs = %+v, want continuation", store.fanoutInputs)
	}
	continuation := store.fanoutInputs[0]
	if continuation.AfterAgentID != 10 || continuation.AfterDeploymentID != 20 {
		t.Fatalf("continuation = %+v", continuation)
	}
	if len(store.completed) != 1 || store.completed[0] != 3 {
		t.Fatalf("completed = %+v, want [3]", store.completed)
	}
}

func TestWorkerFanoutCompletesWhenNoMatches(t *testing.T) {
	store := &fakeWorkerStore{
		jobs: []db.SkillPrewarmJob{{
			ID:          4,
			ExternalID:  "job_4",
			WorkspaceID: 42,
			Payload:     json.RawMessage(`{"kind":"fanout","skill_id":"skill_1","version":"20260708"}`),
		}},
	}
	worker := NewWorker(store, store, store, &fakeResolver{}, &fakePreparer{})

	if err := worker.RunOnce(context.Background(), "worker_1"); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if len(store.snapshotInputs) != 0 {
		t.Fatalf("snapshot inputs = %+v, want none", store.snapshotInputs)
	}
	if len(store.fanoutInputs) != 0 {
		t.Fatalf("fanout inputs = %+v, want no continuation", store.fanoutInputs)
	}
	if len(store.completed) != 1 || store.completed[0] != 4 {
		t.Fatalf("completed = %+v, want [4]", store.completed)
	}
	if len(store.failed) != 0 {
		t.Fatalf("failed = %+v, want none", store.failed)
	}
}

type fakeWorkerStore struct {
	jobs           []db.SkillPrewarmJob
	completed      []int64
	completedBy    []string
	failed         []fakeFailure
	snapshotInputs []db.SkillPrewarmSnapshotJobInput
	fanoutInputs   []db.SkillPrewarmFanoutJobInput
	agents         []db.Agent
	deployments    []db.Deployment
	hasMoreAgents  bool
	hasMoreDeps    bool
}

type fakeFailure struct {
	id          int64
	workerID    string
	attempts    int
	reason      string
	delay       time.Duration
	maxAttempts int
}

func (s *fakeWorkerStore) LeaseSkillPrewarmJobs(_ context.Context, _ string, _ int, _ time.Duration) ([]db.SkillPrewarmJob, error) {
	jobs := s.jobs
	s.jobs = nil
	return jobs, nil
}

func (s *fakeWorkerStore) CompleteSkillPrewarmJob(_ context.Context, jobID int64, workerID string) error {
	s.completed = append(s.completed, jobID)
	s.completedBy = append(s.completedBy, workerID)
	return nil
}

func (s *fakeWorkerStore) FailSkillPrewarmJob(_ context.Context, jobID int64, workerID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	s.failed = append(s.failed, fakeFailure{id: jobID, workerID: workerID, attempts: attempts, reason: reason, delay: retryDelay, maxAttempts: maxAttempts})
	return nil
}

func (s *fakeWorkerStore) EnqueueSkillPrewarmSnapshotJob(_ context.Context, input db.SkillPrewarmSnapshotJobInput) error {
	s.snapshotInputs = append(s.snapshotInputs, input)
	return nil
}

func (s *fakeWorkerStore) EnqueueSkillPrewarmFanoutJob(_ context.Context, input db.SkillPrewarmFanoutJobInput) error {
	s.fanoutInputs = append(s.fanoutInputs, input)
	return nil
}

func (s *fakeWorkerStore) ListAgentsForSkillPrewarmFanout(_ context.Context, _ int64, _ string, _ int64, _ int) ([]db.Agent, bool, error) {
	return s.agents, s.hasMoreAgents, nil
}

func (s *fakeWorkerStore) ListDeploymentsForSkillPrewarmFanout(_ context.Context, _ int64, _ string, _ int64, _ int) ([]db.Deployment, bool, error) {
	return s.deployments, s.hasMoreDeps, nil
}

type fakeResolver struct {
	runtimeSkills []skillsapi.RuntimeSkill
	workspaceID   int64
	err           error
}

func (r *fakeResolver) ResolveAgentSnapshot(_ context.Context, workspaceID int64, _ json.RawMessage) ([]skillsapi.RuntimeSkill, error) {
	r.workspaceID = workspaceID
	if r.err != nil {
		return nil, r.err
	}
	return r.runtimeSkills, nil
}

type fakePreparer struct {
	received []skillsapi.RuntimeSkill
	err      error
}

func (p *fakePreparer) PrepareSkillMount(_ context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*e2bruntime.SkillMount, error) {
	p.received = append([]skillsapi.RuntimeSkill(nil), runtimeSkills...)
	if p.err != nil {
		return nil, p.err
	}
	return &e2bruntime.SkillMount{MountPath: e2bruntime.SandboxSkillsMountPath}, nil
}
