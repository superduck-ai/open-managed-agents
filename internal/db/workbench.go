package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type workbenchPromptRow struct {
	OrgUUID               string         `db:"org_uuid"`
	PromptUUID            string         `db:"prompt_uuid"`
	WorkspaceID           string         `db:"workspace_id"`
	Name                  string         `db:"name"`
	IsSharedWithWorkspace bool           `db:"is_shared_with_workspace"`
	LatestRevisionUUID    sql.NullString `db:"latest_revision_uuid"`
	DeletedAt             sql.NullTime   `db:"deleted_at"`
	CreatedAt             time.Time      `db:"created_at"`
	UpdatedAt             time.Time      `db:"updated_at"`
}

type workbenchRevisionRow struct {
	OrgUUID      string    `db:"org_uuid"`
	PromptUUID   string    `db:"prompt_uuid"`
	RevisionUUID string    `db:"revision_uuid"`
	PayloadJSON  string    `db:"payload_json"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

type workbenchKVRow struct {
	OrgUUID     string         `db:"org_uuid"`
	PromptUUID  string         `db:"prompt_uuid"`
	Key         string         `db:"key"`
	Value       string         `db:"value"`
	VersionJSON sql.NullString `db:"version_json"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}

type workbenchEvaluationRow struct {
	OrgUUID        string    `db:"org_uuid"`
	RevisionUUID   string    `db:"revision_uuid"`
	EvaluationUUID string    `db:"evaluation_uuid"`
	PayloadJSON    string    `db:"payload_json"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type workbenchGeneratedTestCaseRow struct {
	ID         int64  `db:"id"`
	ValuesJSON string `db:"values_json"`
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return platform.ErrNotFound
	}
	return err
}

func (d *DB) GetWorkbenchPrompt(ctx context.Context, orgUUID string, promptUUID string) (*platform.WorkbenchPromptRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" {
		return nil, platform.ErrNotFound
	}
	var row workbenchPromptRow
	err := namedGetContext(ctx, d.sql, &row, `
		SELECT org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace, latest_revision_uuid, deleted_at, created_at, updated_at
		FROM workbench_prompts
		WHERE org_uuid = :org_uuid
		  AND prompt_uuid = :prompt_uuid
		LIMIT 1
	`, map[string]any{"org_uuid": orgUUID, "prompt_uuid": promptUUID})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) ListWorkbenchPrompts(ctx context.Context, orgUUID string, workspaceID string) ([]platform.WorkbenchPromptRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = "default"
	}
	var rows []workbenchPromptRow
	err := namedSelectContext(ctx, d.sql, &rows, `
		SELECT org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace, latest_revision_uuid, deleted_at, created_at, updated_at
		FROM workbench_prompts
		WHERE org_uuid = :org_uuid
		  AND workspace_id = :workspace_id
		  AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC
	`, map[string]any{"org_uuid": strings.TrimSpace(orgUUID), "workspace_id": workspaceID})
	if err != nil {
		return nil, err
	}
	records := make([]platform.WorkbenchPromptRecord, len(rows))
	for index := range rows {
		records[index] = rows[index].record()
	}
	return records, nil
}

func (d *DB) UpsertWorkbenchPrompt(ctx context.Context, record platform.WorkbenchPromptRecord) (platform.WorkbenchPromptRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" {
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
	var row workbenchPromptRow
	err := namedGetContext(ctx, d.sql, &row, `
		INSERT INTO workbench_prompts (
			org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace,
			latest_revision_uuid, deleted_at, created_at, updated_at
		)
		VALUES (
			:org_uuid, :prompt_uuid, :workspace_id, :name, :is_shared_with_workspace,
			:latest_revision_uuid, :deleted_at, :created_at, CURRENT_TIMESTAMP
		)
		ON CONFLICT (org_uuid, prompt_uuid) DO UPDATE
		SET workspace_id = EXCLUDED.workspace_id,
		    name = EXCLUDED.name,
		    is_shared_with_workspace = EXCLUDED.is_shared_with_workspace,
		    latest_revision_uuid = EXCLUDED.latest_revision_uuid,
		    deleted_at = EXCLUDED.deleted_at,
		    updated_at = CURRENT_TIMESTAMP
		RETURNING org_uuid, prompt_uuid, workspace_id, name, is_shared_with_workspace, latest_revision_uuid, deleted_at, created_at, updated_at
	`, map[string]any{
		"org_uuid":                 record.OrgUUID,
		"prompt_uuid":              record.PromptUUID,
		"workspace_id":             record.WorkspaceID,
		"name":                     record.Name,
		"is_shared_with_workspace": record.IsSharedWithWorkspace,
		"latest_revision_uuid":     latestRevisionUUID,
		"deleted_at":               deletedAt,
		"created_at":               record.CreatedAt,
	})
	if err != nil {
		return platform.WorkbenchPromptRecord{}, err
	}
	return row.record(), nil
}

func (d *DB) DeleteWorkbenchPromptState(ctx context.Context, orgUUID string, promptUUID string) error {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" {
		return nil
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	arguments := map[string]any{"org_uuid": orgUUID, "prompt_uuid": promptUUID}
	if _, err := namedExecContext(ctx, tx, `
		INSERT INTO workbench_prompts (org_uuid, prompt_uuid, workspace_id, name, deleted_at, created_at, updated_at)
		VALUES (:org_uuid, :prompt_uuid, 'default', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, prompt_uuid) DO UPDATE
		SET name = '',
		    is_shared_with_workspace = FALSE,
		    latest_revision_uuid = NULL,
		    deleted_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
	`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_prompt_revisions WHERE org_uuid = :org_uuid AND prompt_uuid = :prompt_uuid`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_prompt_kv WHERE org_uuid = :org_uuid AND prompt_uuid = :prompt_uuid`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_evaluations WHERE org_uuid = :org_uuid`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_generated_test_cases WHERE org_uuid = :org_uuid`, arguments); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) GetWorkbenchRevision(ctx context.Context, orgUUID string, promptUUID string, revisionUUID string) (*platform.WorkbenchRevisionRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, platform.ErrNotFound
	}
	var row workbenchRevisionRow
	err := namedGetContext(ctx, d.sql, &row, `
		SELECT org_uuid, prompt_uuid, revision_uuid, CAST(payload AS text) AS payload_json, created_at, updated_at
		FROM workbench_prompt_revisions
		WHERE org_uuid = :org_uuid
		  AND prompt_uuid = :prompt_uuid
		  AND revision_uuid = :revision_uuid
		LIMIT 1
	`, map[string]any{"org_uuid": orgUUID, "prompt_uuid": promptUUID, "revision_uuid": revisionUUID})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

const upsertWorkbenchRevisionQuery = `
	INSERT INTO workbench_prompt_revisions (org_uuid, prompt_uuid, revision_uuid, payload, created_at, updated_at)
	VALUES (:org_uuid, :prompt_uuid, :revision_uuid, CAST(:payload AS jsonb), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT (org_uuid, prompt_uuid, revision_uuid) DO UPDATE
	SET payload = EXCLUDED.payload,
	    updated_at = CURRENT_TIMESTAMP
`

func (d *DB) UpsertWorkbenchRevision(ctx context.Context, record platform.WorkbenchRevisionRecord) error {
	if d == nil || d.sql == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" || strings.TrimSpace(record.RevisionUUID) == "" {
		return nil
	}
	payloadJSON, err := marshalWorkbenchJSON(record.Payload, map[string]any{})
	if err != nil {
		return err
	}
	_, err = namedExecContext(ctx, d.sql, upsertWorkbenchRevisionQuery, map[string]any{
		"org_uuid":      strings.TrimSpace(record.OrgUUID),
		"prompt_uuid":   strings.TrimSpace(record.PromptUUID),
		"revision_uuid": strings.TrimSpace(record.RevisionUUID),
		"payload":       payloadJSON,
	})
	return err
}

func (d *DB) ListWorkbenchEvaluationRevisionIDs(ctx context.Context, orgUUID string) ([]string, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, nil
	}
	var revisionIDs []string
	err := namedSelectContext(ctx, d.sql, &revisionIDs, `
		SELECT DISTINCT revision_uuid
		FROM workbench_evaluations
		WHERE org_uuid = :org_uuid
		ORDER BY revision_uuid ASC
	`, map[string]any{"org_uuid": orgUUID})
	if err != nil {
		return nil, err
	}
	filtered := revisionIDs[:0]
	for _, revisionID := range revisionIDs {
		if strings.TrimSpace(revisionID) != "" {
			filtered = append(filtered, revisionID)
		}
	}
	return filtered, nil
}

func (d *DB) GetWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) (*platform.WorkbenchKVRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil, platform.ErrNotFound
	}
	var row workbenchKVRow
	err := namedGetContext(ctx, d.sql, &row, `
		SELECT org_uuid, prompt_uuid, key, value, CAST(version AS text) AS version_json, created_at, updated_at
		FROM workbench_prompt_kv
		WHERE org_uuid = :org_uuid
		  AND prompt_uuid = :prompt_uuid
		  AND key = :key
		LIMIT 1
	`, map[string]any{"org_uuid": orgUUID, "prompt_uuid": promptUUID, "key": key})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) UpsertWorkbenchKV(ctx context.Context, record platform.WorkbenchKVRecord) error {
	if d == nil || d.sql == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" || strings.TrimSpace(record.Key) == "" {
		return nil
	}
	versionJSON, err := marshalWorkbenchNullableJSON(record.Version)
	if err != nil {
		return err
	}
	_, err = namedExecContext(ctx, d.sql, `
		INSERT INTO workbench_prompt_kv (org_uuid, prompt_uuid, key, value, version, created_at, updated_at)
		VALUES (:org_uuid, :prompt_uuid, :key, :value, CAST(:version AS jsonb), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, prompt_uuid, key) DO UPDATE
		SET value = EXCLUDED.value,
		    version = EXCLUDED.version,
		    updated_at = CURRENT_TIMESTAMP
	`, map[string]any{
		"org_uuid":    strings.TrimSpace(record.OrgUUID),
		"prompt_uuid": strings.TrimSpace(record.PromptUUID),
		"key":         strings.TrimSpace(record.Key),
		"value":       record.Value,
		"version":     versionJSON,
	})
	return err
}

func (d *DB) DeleteWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) error {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	_, err := namedExecContext(ctx, d.sql, `
		DELETE FROM workbench_prompt_kv
		WHERE org_uuid = :org_uuid
		  AND prompt_uuid = :prompt_uuid
		  AND key = :key
	`, map[string]any{"org_uuid": orgUUID, "prompt_uuid": promptUUID, "key": key})
	return err
}

func (d *DB) ListWorkbenchEvaluations(ctx context.Context, orgUUID string, revisionUUID string) ([]platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, nil
	}
	var rows []workbenchEvaluationRow
	err := namedSelectContext(ctx, d.sql, &rows, `
		SELECT org_uuid, revision_uuid, evaluation_uuid, CAST(payload AS text) AS payload_json, created_at, updated_at
		FROM workbench_evaluations
		WHERE org_uuid = :org_uuid
		  AND revision_uuid = :revision_uuid
		ORDER BY created_at ASC, id ASC
	`, map[string]any{"org_uuid": orgUUID, "revision_uuid": revisionUUID})
	if err != nil {
		return nil, err
	}
	records := make([]platform.WorkbenchEvaluationRecord, len(rows))
	for index := range rows {
		records[index] = rows[index].record()
	}
	return records, nil
}

func (d *DB) GetWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	var row workbenchEvaluationRow
	err := namedGetContext(ctx, d.sql, &row, `
		SELECT org_uuid, revision_uuid, evaluation_uuid, CAST(payload AS text) AS payload_json, created_at, updated_at
		FROM workbench_evaluations
		WHERE org_uuid = :org_uuid
		  AND evaluation_uuid = :evaluation_uuid
		LIMIT 1
	`, map[string]any{"org_uuid": orgUUID, "evaluation_uuid": evaluationUUID})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) UpsertWorkbenchEvaluation(ctx context.Context, record platform.WorkbenchEvaluationRecord) error {
	if d == nil || d.sql == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.RevisionUUID) == "" || strings.TrimSpace(record.EvaluationUUID) == "" {
		return nil
	}
	payloadJSON, err := marshalWorkbenchJSON(record.Payload, map[string]any{})
	if err != nil {
		return err
	}
	_, err = namedExecContext(ctx, d.sql, `
		INSERT INTO workbench_evaluations (org_uuid, revision_uuid, evaluation_uuid, payload, created_at, updated_at)
		VALUES (:org_uuid, :revision_uuid, :evaluation_uuid, CAST(:payload AS jsonb), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (org_uuid, evaluation_uuid) DO UPDATE
		SET revision_uuid = EXCLUDED.revision_uuid,
		    payload = EXCLUDED.payload,
		    updated_at = CURRENT_TIMESTAMP
	`, map[string]any{
		"org_uuid":        strings.TrimSpace(record.OrgUUID),
		"revision_uuid":   strings.TrimSpace(record.RevisionUUID),
		"evaluation_uuid": strings.TrimSpace(record.EvaluationUUID),
		"payload":         payloadJSON,
	})
	return err
}

func (d *DB) DeleteWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	var row workbenchEvaluationRow
	err := namedGetContext(ctx, d.sql, &row, `
		DELETE FROM workbench_evaluations
		WHERE org_uuid = :org_uuid
		  AND evaluation_uuid = :evaluation_uuid
		RETURNING org_uuid, revision_uuid, evaluation_uuid, CAST(payload AS text) AS payload_json, created_at, updated_at
	`, map[string]any{"org_uuid": orgUUID, "evaluation_uuid": evaluationUUID})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) AppendWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, values map[string]any) error {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || len(values) == 0 {
		return nil
	}
	valuesJSON, err := marshalWorkbenchJSON(values, map[string]any{})
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	arguments := map[string]any{"org_uuid": orgUUID, "values": valuesJSON}
	if _, err := namedExecContext(ctx, tx, `
		INSERT INTO workbench_generated_test_cases (org_uuid, values, created_at)
		VALUES (:org_uuid, CAST(:values AS jsonb), CURRENT_TIMESTAMP)
	`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `
		DELETE FROM workbench_generated_test_cases
		WHERE org_uuid = :org_uuid
		  AND id NOT IN (
		      SELECT id
		      FROM workbench_generated_test_cases
		      WHERE org_uuid = :org_uuid
		      ORDER BY id DESC
		      LIMIT 10
		  )
	`, arguments); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) TakeWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, requested map[string]any) (map[string]any, bool, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || len(requested) == 0 {
		return nil, false, nil
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var rows []workbenchGeneratedTestCaseRow
	err = namedSelectContext(ctx, tx, &rows, `
		SELECT id, CAST(values AS text) AS values_json
		FROM workbench_generated_test_cases
		WHERE org_uuid = :org_uuid
		ORDER BY id ASC
		FOR UPDATE
	`, map[string]any{"org_uuid": orgUUID})
	if err != nil {
		return nil, false, err
	}

	var selectedID int64
	var selectedValues map[string]any
	for _, row := range rows {
		values := parseWorkbenchMapJSON(row.ValuesJSON)
		if workbenchGeneratedValuesMatchRequest(values, requested) {
			selectedID = row.ID
			selectedValues = values
			break
		}
	}
	if selectedID == 0 {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_generated_test_cases WHERE id = :id`, map[string]any{
		"id": selectedID,
	}); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return selectedValues, true, nil
}

func (r workbenchPromptRow) record() platform.WorkbenchPromptRecord {
	record := platform.WorkbenchPromptRecord{
		OrgUUID:               r.OrgUUID,
		PromptUUID:            r.PromptUUID,
		WorkspaceID:           r.WorkspaceID,
		Name:                  r.Name,
		IsSharedWithWorkspace: r.IsSharedWithWorkspace,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}
	if r.LatestRevisionUUID.Valid {
		value := r.LatestRevisionUUID.String
		record.LatestRevisionUUID = &value
	}
	if r.DeletedAt.Valid {
		value := r.DeletedAt.Time
		record.DeletedAt = &value
	}
	return record
}

func (r workbenchRevisionRow) record() platform.WorkbenchRevisionRecord {
	return platform.WorkbenchRevisionRecord{
		OrgUUID:      r.OrgUUID,
		PromptUUID:   r.PromptUUID,
		RevisionUUID: r.RevisionUUID,
		Payload:      parseWorkbenchMapJSON(r.PayloadJSON),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func (r workbenchKVRow) record() platform.WorkbenchKVRecord {
	record := platform.WorkbenchKVRecord{
		OrgUUID:    r.OrgUUID,
		PromptUUID: r.PromptUUID,
		Key:        r.Key,
		Value:      r.Value,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
	if r.VersionJSON.Valid && strings.TrimSpace(r.VersionJSON.String) != "" {
		var version any
		if err := json.Unmarshal([]byte(r.VersionJSON.String), &version); err == nil {
			record.Version = version
		}
	}
	return record
}

func (r workbenchEvaluationRow) record() platform.WorkbenchEvaluationRecord {
	return platform.WorkbenchEvaluationRecord{
		OrgUUID:        r.OrgUUID,
		RevisionUUID:   r.RevisionUUID,
		EvaluationUUID: r.EvaluationUUID,
		Payload:        parseWorkbenchMapJSON(r.PayloadJSON),
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
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
