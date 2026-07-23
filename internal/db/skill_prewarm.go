package db

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"
)

type SkillPrewarmJob struct {
	ID          int64
	ExternalID  string
	WorkspaceID int64
	Payload     json.RawMessage
	Attempts    int
}

type SkillPrewarmSnapshotJobInput struct {
	WorkspaceID         int64
	AgentSnapshot       json.RawMessage
	Source              string
	SourceID            string
	Trigger             string
	TriggerSkillID      string
	TriggerSkillVersion string
}

type SkillPrewarmFanoutJobInput struct {
	WorkspaceID       int64
	SkillID           string
	Version           string
	AfterAgentID      int64
	AfterDeploymentID int64
}

type skillPrewarmSnapshotPayload struct {
	Kind                string          `json:"kind"`
	AgentSnapshot       json.RawMessage `json:"agent_snapshot"`
	Source              string          `json:"source"`
	SourceID            string          `json:"source_id"`
	Trigger             string          `json:"trigger"`
	TriggerSkillID      string          `json:"trigger_skill_id,omitempty"`
	TriggerSkillVersion string          `json:"trigger_skill_version,omitempty"`
}

type skillPrewarmFanoutPayload struct {
	Kind              string `json:"kind"`
	SkillID           string `json:"skill_id"`
	Version           string `json:"version"`
	AfterAgentID      int64  `json:"after_agent_id,omitempty"`
	AfterDeploymentID int64  `json:"after_deployment_id,omitempty"`
}

func (d *DB) EnqueueSkillPrewarmSnapshotJob(ctx context.Context, input SkillPrewarmSnapshotJobInput) error {
	payload, externalID, err := skillPrewarmSnapshotJobPayload(input)
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values ($1, $2, 'skill_prewarm', 'pending', $3::jsonb)
		on conflict (external_id) do nothing
	`, externalID, input.WorkspaceID, jsonArg(payload))
	return err
}

func (d *DB) EnqueueSkillPrewarmFanoutJob(ctx context.Context, input SkillPrewarmFanoutJobInput) error {
	payload, externalID, err := skillPrewarmFanoutJobPayload(input)
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values ($1, $2, 'skill_prewarm', 'pending', $3::jsonb)
		on conflict (external_id) do nothing
	`, externalID, input.WorkspaceID, jsonArg(payload))
	return err
}

func (d *DB) LeaseSkillPrewarmJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]SkillPrewarmJob, error) {
	if limit <= 0 {
		limit = 5
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	rows, err := d.Pool.Query(ctx, `
		with next_jobs as (
			select id
			from jobs
			where type = 'skill_prewarm'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit $1
			for update skip locked
		)
		update jobs j
		set status = 'running',
			locked_by = $2,
			locked_until = now() + $3::interval,
			updated_at = now()
		from next_jobs
		where j.id = next_jobs.id
		returning j.id, j.external_id, j.workspace_id, j.payload, j.attempts
	`, limit, workerID, leaseDuration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []SkillPrewarmJob
	for rows.Next() {
		var job SkillPrewarmJob
		var payload []byte
		if err := rows.Scan(&job.ID, &job.ExternalID, &job.WorkspaceID, &payload, &job.Attempts); err != nil {
			return nil, err
		}
		job.Payload = copyRaw(payload)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) CompleteSkillPrewarmJob(ctx context.Context, jobID int64, workerID string) error {
	tag, err := d.Pool.Exec(ctx, `
			update jobs
			set status = 'completed',
				locked_by = null,
				locked_until = null,
				updated_at = now()
			where id = $1
				and type = 'skill_prewarm'
				and status = 'running'
				and locked_by = $2
		`, jobID, workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) FailSkillPrewarmJob(ctx context.Context, jobID int64, workerID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	now := time.Now().UTC()
	runAfter := now.Add(retryDelay)
	tag, err := d.Pool.Exec(ctx, `
			update jobs
			set status = $2,
				locked_by = null,
				locked_until = null,
			run_after = $3,
			updated_at = now(),
			attempts = $5,
			payload = payload || jsonb_build_object(
					'last_error', $4::text,
					'last_error_at', $6::text
				)
			where id = $1
				and type = 'skill_prewarm'
				and status = 'running'
				and locked_by = $7
		`, jobID, status, runAfter, reason, nextAttempts, now.Format(time.RFC3339Nano), workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListAgentsForSkillPrewarmFanout(ctx context.Context, workspaceID int64, skillID string, afterID int64, limit int) ([]Agent, bool, error) {
	if limit <= 0 {
		limit = 100
	}
	agents, err := selectAgentsSQLX(ctx, d.sql, agentSelectSQL()+`
		where workspace_id = :workspace_id
			and id > :after_id
			and deleted_at is null
				and archived_at is null
				and exists (
					select 1
					from jsonb_array_elements(coalesce(skills, CAST('[]' AS jsonb))) elem
					where elem->>'type' = 'custom'
						and elem->>'skill_id' = :skill_id
						and coalesce(nullif(elem->>'version', ''), 'latest') = 'latest'
			)
		order by id asc
		limit :limit
	`, map[string]any{
		"workspace_id": workspaceID,
		"skill_id":     skillID,
		"after_id":     afterID,
		"limit":        limit + 1,
	})
	if err != nil {
		return nil, false, err
	}
	hasMore := len(agents) > limit
	if hasMore {
		agents = agents[:limit]
	}
	return agents, hasMore, nil
}

func (d *DB) ListDeploymentsForSkillPrewarmFanout(ctx context.Context, workspaceID int64, skillID string, afterID int64, limit int) ([]Deployment, bool, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.Pool.Query(ctx, `
		select `+deploymentColumns()+`
		from deployments
		where workspace_id = $1
			and id > $3
			and deleted_at is null
			and archived_at is null
			and status = 'active'
			and exists (
				select 1
				from jsonb_array_elements(coalesce(agent_snapshot->'skills', '[]'::jsonb)) elem
				where elem->>'type' = 'custom'
					and elem->>'skill_id' = $2
					and coalesce(nullif(elem->>'version', ''), 'latest') = 'latest'
			)
		order by id asc
		limit $4
	`, workspaceID, skillID, afterID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	deployments, err := scanDeploymentRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(deployments) > limit
	if hasMore {
		deployments = deployments[:limit]
	}
	return deployments, hasMore, nil
}

func skillPrewarmSnapshotJobPayload(input SkillPrewarmSnapshotJobInput) (json.RawMessage, string, error) {
	payload, err := json.Marshal(skillPrewarmSnapshotPayload{
		Kind:                "snapshot",
		AgentSnapshot:       copyRaw(input.AgentSnapshot),
		Source:              input.Source,
		SourceID:            input.SourceID,
		Trigger:             input.Trigger,
		TriggerSkillID:      input.TriggerSkillID,
		TriggerSkillVersion: input.TriggerSkillVersion,
	})
	if err != nil {
		return nil, "", err
	}
	return payload, skillPrewarmExternalID(input.WorkspaceID, payload), nil
}

func skillPrewarmFanoutJobPayload(input SkillPrewarmFanoutJobInput) (json.RawMessage, string, error) {
	payload, err := json.Marshal(skillPrewarmFanoutPayload{
		Kind:              "fanout",
		SkillID:           input.SkillID,
		Version:           input.Version,
		AfterAgentID:      input.AfterAgentID,
		AfterDeploymentID: input.AfterDeploymentID,
	})
	if err != nil {
		return nil, "", err
	}
	return payload, skillPrewarmExternalID(input.WorkspaceID, payload), nil
}

func skillPrewarmExternalID(workspaceID int64, payload json.RawMessage) string {
	var workspace [8]byte
	binary.LittleEndian.PutUint64(workspace[:], uint64(workspaceID))
	hash := sha256.New()
	hash.Write([]byte("skill_prewarm:"))
	hash.Write(workspace[:])
	hash.Write(payload)
	return "job_skpw_" + hex.EncodeToString(hash.Sum(nil))[:40]
}
