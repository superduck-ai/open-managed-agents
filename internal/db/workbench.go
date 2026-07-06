package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/jackc/pgx/v5"
)

type workbenchScanner interface {
	Scan(dest ...any) error
}

func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return platform.ErrNotFound
	}
	return err
}

func (d *DB) GetWorkbenchPrompt(ctx context.Context, orgUUID string, promptUUID string) (*platform.WorkbenchPromptRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" {
		return nil, platform.ErrNotFound
	}
	record, err := scanWorkbenchPrompt(d.Pool.QueryRow(ctx, `
		SELECT org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace, latest_revision_uuid, deleted_at, created_at, updated_at
		FROM workbench_prompts
		WHERE org_uuid = $1
		  AND prompt_uuid = $2
		LIMIT 1
	`, orgUUID, promptUUID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &record, nil
}

func (d *DB) ListWorkbenchPrompts(ctx context.Context, orgUUID string, workspaceID string) ([]platform.WorkbenchPromptRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = "default"
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace, latest_revision_uuid, deleted_at, created_at, updated_at
		FROM workbench_prompts
		WHERE org_uuid = $1
		  AND workspace_id = $2
		  AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC
	`, strings.TrimSpace(orgUUID), workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []platform.WorkbenchPromptRecord{}
	for rows.Next() {
		record, err := scanWorkbenchPrompt(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) UpsertWorkbenchPrompt(ctx context.Context, record platform.WorkbenchPromptRecord) (platform.WorkbenchPromptRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" {
		return platform.WorkbenchPromptRecord{}, platform.ErrNotFound
	}
	record.OrgUUID = strings.TrimSpace(record.OrgUUID)
	record.PromptUUID = strings.TrimSpace(record.PromptUUID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	if record.WorkspaceID == "" {
		record.WorkspaceID = "default"
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	var latestRevisionUUID any
	if record.LatestRevisionUUID != nil && strings.TrimSpace(*record.LatestRevisionUUID) != "" {
		latestRevisionUUID = strings.TrimSpace(*record.LatestRevisionUUID)
	}
	var deletedAt any
	if record.DeletedAt != nil && !record.DeletedAt.IsZero() {
		deletedAt = record.DeletedAt.UTC()
	}
	saved, err := scanWorkbenchPrompt(d.Pool.QueryRow(ctx, `
		INSERT INTO workbench_prompts (
			org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace,
			latest_revision_uuid, deleted_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, prompt_uuid) DO UPDATE
		SET workspace_id = EXCLUDED.workspace_id,
		    name = EXCLUDED.name,
		    is_shared_with_workspace = EXCLUDED.is_shared_with_workspace,
		    latest_revision_uuid = EXCLUDED.latest_revision_uuid,
		    deleted_at = EXCLUDED.deleted_at,
		    updated_at = CURRENT_TIMESTAMP
		RETURNING org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace, latest_revision_uuid, deleted_at, created_at, updated_at
	`, record.OrgUUID, record.PromptUUID, record.WorkspaceID, record.Name, record.IsSharedWithWorkspace, latestRevisionUUID, deletedAt, record.CreatedAt))
	if err != nil {
		return platform.WorkbenchPromptRecord{}, err
	}
	return saved, nil
}

func (d *DB) DeleteWorkbenchPromptState(ctx context.Context, orgUUID string, promptUUID string) error {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" {
		return nil
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO workbench_prompts (org_uuid, prompt_uuid, workspace_id, name, deleted_at, created_at, updated_at)
		VALUES ($1, $2, 'default', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, prompt_uuid) DO UPDATE
		SET name = '',
		    is_shared_with_workspace = FALSE,
		    latest_revision_uuid = NULL,
		    deleted_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
	`, orgUUID, promptUUID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workbench_prompt_revisions WHERE org_uuid = $1 AND prompt_uuid = $2`, orgUUID, promptUUID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workbench_prompt_kv WHERE org_uuid = $1 AND prompt_uuid = $2`, orgUUID, promptUUID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workbench_evaluations WHERE org_uuid = $1`, orgUUID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workbench_generated_test_cases WHERE org_uuid = $1`, orgUUID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) GetWorkbenchRevision(ctx context.Context, orgUUID string, promptUUID string, revisionUUID string) (*platform.WorkbenchRevisionRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, platform.ErrNotFound
	}
	var record platform.WorkbenchRevisionRecord
	var payloadJSON string
	err := d.Pool.QueryRow(ctx, `
		SELECT org_uuid, prompt_uuid, revision_uuid, payload::text, created_at, updated_at
		FROM workbench_prompt_revisions
		WHERE org_uuid = $1
		  AND prompt_uuid = $2
		  AND revision_uuid = $3
		LIMIT 1
	`, orgUUID, promptUUID, revisionUUID).Scan(&record.OrgUUID, &record.PromptUUID, &record.RevisionUUID, &payloadJSON, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	record.Payload = parseWorkbenchMapJSON(payloadJSON)
	return &record, nil
}

func (d *DB) UpsertWorkbenchRevision(ctx context.Context, record platform.WorkbenchRevisionRecord) error {
	if d == nil || d.Pool == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" || strings.TrimSpace(record.RevisionUUID) == "" {
		return nil
	}
	payloadJSON, err := marshalWorkbenchJSON(record.Payload, map[string]any{})
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `
		INSERT INTO workbench_prompt_revisions (org_uuid, prompt_uuid, revision_uuid, payload, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, prompt_uuid, revision_uuid) DO UPDATE
		SET payload = EXCLUDED.payload,
		    updated_at = CURRENT_TIMESTAMP
	`, strings.TrimSpace(record.OrgUUID), strings.TrimSpace(record.PromptUUID), strings.TrimSpace(record.RevisionUUID), payloadJSON)
	return err
}

func (d *DB) ListWorkbenchEvaluationRevisionIDs(ctx context.Context, orgUUID string) ([]string, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, nil
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT revision_uuid
		FROM workbench_evaluations
		WHERE org_uuid = $1
		ORDER BY revision_uuid ASC
	`, orgUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisionIDs []string
	for rows.Next() {
		var revisionID string
		if err := rows.Scan(&revisionID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(revisionID) != "" {
			revisionIDs = append(revisionIDs, revisionID)
		}
	}
	return revisionIDs, rows.Err()
}

func (d *DB) GetWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) (*platform.WorkbenchKVRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil, platform.ErrNotFound
	}
	record, err := scanWorkbenchKV(d.Pool.QueryRow(ctx, `
		SELECT org_uuid, prompt_uuid, key, value, version::text, created_at, updated_at
		FROM workbench_prompt_kv
		WHERE org_uuid = $1
		  AND prompt_uuid = $2
		  AND key = $3
		LIMIT 1
	`, orgUUID, promptUUID, key))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &record, nil
}

func (d *DB) UpsertWorkbenchKV(ctx context.Context, record platform.WorkbenchKVRecord) error {
	if d == nil || d.Pool == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" || strings.TrimSpace(record.Key) == "" {
		return nil
	}
	versionJSON, err := marshalWorkbenchNullableJSON(record.Version)
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `
		INSERT INTO workbench_prompt_kv (org_uuid, prompt_uuid, key, value, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, prompt_uuid, key) DO UPDATE
		SET value = EXCLUDED.value,
		    version = EXCLUDED.version,
		    updated_at = CURRENT_TIMESTAMP
	`, strings.TrimSpace(record.OrgUUID), strings.TrimSpace(record.PromptUUID), strings.TrimSpace(record.Key), record.Value, versionJSON)
	return err
}

func (d *DB) DeleteWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) error {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	_, err := d.Pool.Exec(ctx, `
		DELETE FROM workbench_prompt_kv
		WHERE org_uuid = $1
		  AND prompt_uuid = $2
		  AND key = $3
	`, orgUUID, promptUUID, key)
	return err
}

func (d *DB) ListWorkbenchEvaluations(ctx context.Context, orgUUID string, revisionUUID string) ([]platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, nil
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT org_uuid, revision_uuid, evaluation_uuid, payload::text, created_at, updated_at
		FROM workbench_evaluations
		WHERE org_uuid = $1
		  AND revision_uuid = $2
		ORDER BY created_at ASC, id ASC
	`, orgUUID, revisionUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []platform.WorkbenchEvaluationRecord
	for rows.Next() {
		record, err := scanWorkbenchEvaluationRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) GetWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	record, err := scanWorkbenchEvaluation(d.Pool.QueryRow(ctx, `
		SELECT org_uuid, revision_uuid, evaluation_uuid, payload::text, created_at, updated_at
		FROM workbench_evaluations
		WHERE org_uuid = $1
		  AND evaluation_uuid = $2
		LIMIT 1
	`, orgUUID, evaluationUUID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &record, nil
}

func (d *DB) UpsertWorkbenchEvaluation(ctx context.Context, record platform.WorkbenchEvaluationRecord) error {
	if d == nil || d.Pool == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.RevisionUUID) == "" || strings.TrimSpace(record.EvaluationUUID) == "" {
		return nil
	}
	payloadJSON, err := marshalWorkbenchJSON(record.Payload, map[string]any{})
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `
		INSERT INTO workbench_evaluations (org_uuid, revision_uuid, evaluation_uuid, payload, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, evaluation_uuid) DO UPDATE
		SET revision_uuid = EXCLUDED.revision_uuid,
		    payload = EXCLUDED.payload,
		    updated_at = CURRENT_TIMESTAMP
	`, strings.TrimSpace(record.OrgUUID), strings.TrimSpace(record.RevisionUUID), strings.TrimSpace(record.EvaluationUUID), payloadJSON)
	return err
}

func (d *DB) DeleteWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	record, err := scanWorkbenchEvaluation(d.Pool.QueryRow(ctx, `
		DELETE FROM workbench_evaluations
		WHERE org_uuid = $1
		  AND evaluation_uuid = $2
		RETURNING org_uuid, revision_uuid, evaluation_uuid, payload::text, created_at, updated_at
	`, orgUUID, evaluationUUID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &record, nil
}

func (d *DB) AppendWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, values map[string]any) error {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || len(values) == 0 {
		return nil
	}
	valuesJSON, err := marshalWorkbenchJSON(values, map[string]any{})
	if err != nil {
		return err
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO workbench_generated_test_cases (org_uuid, values, created_at)
		VALUES ($1, $2::jsonb, CURRENT_TIMESTAMP)
	`, orgUUID, valuesJSON); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM workbench_generated_test_cases
		WHERE org_uuid = $1
		  AND id NOT IN (
		      SELECT id
		      FROM workbench_generated_test_cases
		      WHERE org_uuid = $1
		      ORDER BY id DESC
		      LIMIT 10
		  )
	`, orgUUID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) TakeWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, requested map[string]any) (map[string]any, bool, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || len(requested) == 0 {
		return nil, false, nil
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, values::text
		FROM workbench_generated_test_cases
		WHERE org_uuid = $1
		ORDER BY id ASC
		FOR UPDATE
	`, orgUUID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var selectedID int64
	var selectedValues map[string]any
	for rows.Next() {
		var id int64
		var valuesJSON string
		if err := rows.Scan(&id, &valuesJSON); err != nil {
			return nil, false, err
		}
		values := parseWorkbenchMapJSON(valuesJSON)
		if workbenchGeneratedValuesMatchRequest(values, requested) {
			selectedID = id
			selectedValues = values
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	rows.Close()
	if selectedID == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workbench_generated_test_cases WHERE id = $1`, selectedID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return selectedValues, true, nil
}

func scanWorkbenchPrompt(row workbenchScanner) (platform.WorkbenchPromptRecord, error) {
	var record platform.WorkbenchPromptRecord
	var latestRevisionUUID sql.NullString
	var deletedAt sql.NullTime
	if err := row.Scan(
		&record.OrgUUID,
		&record.PromptUUID,
		&record.WorkspaceID,
		&record.Name,
		&record.IsSharedWithWorkspace,
		&latestRevisionUUID,
		&deletedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return platform.WorkbenchPromptRecord{}, err
	}
	if latestRevisionUUID.Valid {
		value := latestRevisionUUID.String
		record.LatestRevisionUUID = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time
		record.DeletedAt = &value
	}
	return record, nil
}

func scanWorkbenchKV(row workbenchScanner) (platform.WorkbenchKVRecord, error) {
	var record platform.WorkbenchKVRecord
	var versionJSON sql.NullString
	if err := row.Scan(&record.OrgUUID, &record.PromptUUID, &record.Key, &record.Value, &versionJSON, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return platform.WorkbenchKVRecord{}, err
	}
	if versionJSON.Valid && strings.TrimSpace(versionJSON.String) != "" {
		var version any
		if err := json.Unmarshal([]byte(versionJSON.String), &version); err == nil {
			record.Version = version
		}
	}
	return record, nil
}

func scanWorkbenchEvaluation(row workbenchScanner) (platform.WorkbenchEvaluationRecord, error) {
	return scanWorkbenchEvaluationRows(row)
}

func scanWorkbenchEvaluationRows(row workbenchScanner) (platform.WorkbenchEvaluationRecord, error) {
	var record platform.WorkbenchEvaluationRecord
	var payloadJSON string
	if err := row.Scan(&record.OrgUUID, &record.RevisionUUID, &record.EvaluationUUID, &payloadJSON, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return platform.WorkbenchEvaluationRecord{}, err
	}
	record.Payload = parseWorkbenchMapJSON(payloadJSON)
	return record, nil
}

func marshalWorkbenchJSON(value any, fallback any) (string, error) {
	if value == nil {
		value = fallback
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func marshalWorkbenchNullableJSON(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func parseWorkbenchMapJSON(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func workbenchGeneratedValuesMatchRequest(values map[string]any, requested map[string]any) bool {
	if len(values) == 0 || len(requested) == 0 {
		return false
	}
	for name := range requested {
		if _, ok := values[name]; !ok {
			return false
		}
	}
	return true
}
