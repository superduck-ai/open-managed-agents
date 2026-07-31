package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
)

type workbenchPromptRow struct {
	OrgUUID               uuid.UUID      `db:"organization_uuid"`
	PromptUUID            string         `db:"prompt_uuid"`
	WorkspaceUUID         uuid.UUID      `db:"workspace_uuid"`
	WorkspaceDisplayID    string         `db:"workspace_display_id"`
	Name                  string         `db:"name"`
	IsSharedWithWorkspace bool           `db:"is_shared_with_workspace"`
	LatestRevisionUUID    sql.NullString `db:"latest_revision_uuid"`
	DeletedAt             sql.NullTime   `db:"deleted_at"`
	CreatedAt             time.Time      `db:"created_at"`
	UpdatedAt             time.Time      `db:"updated_at"`
}

type workbenchRevisionRow struct {
	OrgUUID      uuid.UUID `db:"organization_uuid"`
	PromptUUID   string    `db:"prompt_uuid"`
	RevisionUUID string    `db:"revision_uuid"`
	PayloadJSON  string    `db:"payload_json"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

type workbenchKVRow struct {
	OrgUUID     uuid.UUID      `db:"organization_uuid"`
	PromptUUID  string         `db:"prompt_uuid"`
	Key         string         `db:"key"`
	Value       string         `db:"value"`
	VersionJSON sql.NullString `db:"version_json"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}

type workbenchEvaluationRow struct {
	OrgUUID        uuid.UUID `db:"organization_uuid"`
	RevisionUUID   string    `db:"revision_uuid"`
	EvaluationUUID string    `db:"evaluation_uuid"`
	PayloadJSON    string    `db:"payload_json"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type workbenchGeneratedTestCaseRow struct {
	UUID       uuid.UUID `db:"uuid"`
	ValuesJSON string    `db:"values_json"`
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return platform.ErrNotFound
	}
	return err
}

func workbenchUpsertResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return platform.ErrNotFound
	}
	return nil
}

func (d *DB) GetWorkbenchPrompt(ctx context.Context, orgUUID string, promptUUID string) (*platform.WorkbenchPromptRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	var row workbenchPromptRow
	err = namedGetContext(ctx, d.sql, &row, `
		SELECT organization_uuid, prompt_uuid,
		       workspace_uuid, workspace_display_id,
		       name, is_shared_with_workspace, latest_revision_uuid,
		       deleted_at, created_at, updated_at
		FROM workbench_prompts
		WHERE organization_uuid = :organization_uuid
		  AND prompt_uuid = :prompt_uuid
		LIMIT 1
	`, map[string]any{"organization_uuid": typedOrgUUID, "prompt_uuid": promptUUID})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) ListWorkbenchPrompts(ctx context.Context, orgUUID string, workspaceUUID string) ([]platform.WorkbenchPromptRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceUUID) == "" {
		return nil, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, nil
	}
	typedWorkspaceUUID, err := parseDBUUID("workspace_uuid", workspaceUUID)
	if err != nil {
		return nil, nil
	}
	var rows []workbenchPromptRow
	err = namedSelectContext(ctx, d.sql, &rows, `
		SELECT organization_uuid, prompt_uuid,
		       workspace_uuid, workspace_display_id,
		       name, is_shared_with_workspace, latest_revision_uuid,
		       deleted_at, created_at, updated_at
		FROM workbench_prompts
		WHERE organization_uuid = :organization_uuid
		  AND workspace_uuid = :workspace_uuid
		  AND deleted_at IS NULL
		ORDER BY updated_at DESC, uuid DESC
	`, map[string]any{
		"organization_uuid": typedOrgUUID,
		"workspace_uuid":    typedWorkspaceUUID,
	})
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
	record.WorkspaceUUID = strings.TrimSpace(record.WorkspaceUUID)
	record.WorkspaceDisplayID = strings.TrimSpace(record.WorkspaceDisplayID)
	if record.WorkspaceUUID == "" {
		return platform.WorkbenchPromptRecord{}, platform.ErrNotFound
	}
	if record.WorkspaceDisplayID == "" {
		record.WorkspaceDisplayID = record.WorkspaceUUID
	}
	organizationUUID, err := parseDBUUID("organization_uuid", record.OrgUUID)
	if err != nil {
		return platform.WorkbenchPromptRecord{}, platform.ErrNotFound
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", record.WorkspaceUUID)
	if err != nil {
		return platform.WorkbenchPromptRecord{}, platform.ErrNotFound
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
	err = namedGetContext(ctx, d.sql, &row, `
		INSERT INTO workbench_prompts (
			organization_uuid, prompt_uuid, workspace_uuid, workspace_display_id,
			name, is_shared_with_workspace,
			latest_revision_uuid, deleted_at, created_at, updated_at
		)
		VALUES (
			:organization_uuid, :prompt_uuid, :workspace_uuid,
			:workspace_display_id, :name, :is_shared_with_workspace,
			:latest_revision_uuid, :deleted_at, :created_at, CURRENT_TIMESTAMP
		)
		ON CONFLICT (organization_uuid, prompt_uuid) DO UPDATE
		SET workspace_uuid = EXCLUDED.workspace_uuid,
		    workspace_display_id = EXCLUDED.workspace_display_id,
		    name = EXCLUDED.name,
		    is_shared_with_workspace = EXCLUDED.is_shared_with_workspace,
		    latest_revision_uuid = EXCLUDED.latest_revision_uuid,
		    deleted_at = EXCLUDED.deleted_at,
		    updated_at = CURRENT_TIMESTAMP
		RETURNING organization_uuid, prompt_uuid,
		          workspace_uuid, workspace_display_id,
		          name, is_shared_with_workspace, latest_revision_uuid,
		          deleted_at, created_at, updated_at
	`, map[string]any{
		"organization_uuid":        organizationUUID,
		"prompt_uuid":              record.PromptUUID,
		"workspace_uuid":           workspaceUUID,
		"workspace_display_id":     record.WorkspaceDisplayID,
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

func (d *DB) DeleteWorkbenchPromptState(
	ctx context.Context,
	orgUUID string,
	promptUUID string,
	workspaceUUID string,
	workspaceDisplayID string,
) error {
	if d == nil || d.sql == nil ||
		strings.TrimSpace(orgUUID) == "" ||
		strings.TrimSpace(promptUUID) == "" ||
		strings.TrimSpace(workspaceUUID) == "" {
		return nil
	}
	organizationUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil
	}
	typedWorkspaceUUID, err := parseDBUUID("workspace_uuid", workspaceUUID)
	if err != nil {
		return nil
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	arguments := map[string]any{
		"organization_uuid": organizationUUID,
		"workspace_uuid":    typedWorkspaceUUID,
		"workspace_display_id": firstNonEmpty(
			strings.TrimSpace(workspaceDisplayID),
			strings.TrimSpace(workspaceUUID),
		),
		"prompt_uuid": promptUUID,
	}
	var promptRefUUID uuid.UUID
	if err := namedGetContext(ctx, tx, &promptRefUUID, `
		INSERT INTO workbench_prompts (
			organization_uuid, prompt_uuid, workspace_uuid, workspace_display_id,
			name, deleted_at, created_at, updated_at
		)
		VALUES (
			:organization_uuid, :prompt_uuid, :workspace_uuid,
			:workspace_display_id, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		)
		ON CONFLICT (organization_uuid, prompt_uuid) DO UPDATE
		SET name = '',
		    is_shared_with_workspace = FALSE,
		    latest_revision_uuid = NULL,
		    deleted_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		RETURNING uuid
	`, arguments); err != nil {
		return err
	}
	arguments["prompt_ref_uuid"] = promptRefUUID
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_prompt_revisions WHERE prompt_ref_uuid = :prompt_ref_uuid`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_prompt_kv WHERE prompt_ref_uuid = :prompt_ref_uuid`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_evaluations WHERE organization_uuid = :organization_uuid`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_generated_test_cases WHERE organization_uuid = :organization_uuid`, arguments); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) GetWorkbenchRevision(ctx context.Context, orgUUID string, promptUUID string, revisionUUID string) (*platform.WorkbenchRevisionRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	var row workbenchRevisionRow
	err = namedGetContext(ctx, d.sql, &row, `
		SELECT r.organization_uuid,
		       r.prompt_uuid, r.revision_uuid, CAST(r.payload AS text) AS payload_json,
		       r.created_at, r.updated_at
		FROM workbench_prompt_revisions r
		JOIN workbench_prompts p ON p.uuid = r.prompt_ref_uuid
		WHERE r.organization_uuid = :organization_uuid
		  AND p.prompt_uuid = :prompt_uuid
		  AND r.revision_uuid = :revision_uuid
		LIMIT 1
	`, map[string]any{"organization_uuid": typedOrgUUID, "prompt_uuid": promptUUID, "revision_uuid": revisionUUID})
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

const upsertWorkbenchRevisionQuery = `
	WITH target_prompt AS (
		SELECT uuid
		FROM workbench_prompts
		WHERE organization_uuid = :organization_uuid
		  AND prompt_uuid = :prompt_uuid
		LIMIT 1
	)
	INSERT INTO workbench_prompt_revisions (
		organization_uuid, prompt_ref_uuid, prompt_uuid, revision_uuid,
		payload, created_at, updated_at
	)
	SELECT :organization_uuid, target_prompt.uuid, :prompt_uuid,
	       :revision_uuid, CAST(:payload AS jsonb), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
	FROM target_prompt
	ON CONFLICT (organization_uuid, prompt_ref_uuid, revision_uuid) DO UPDATE
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
	organizationUUID, err := parseDBUUID("organization_uuid", record.OrgUUID)
	if err != nil {
		return platform.ErrNotFound
	}
	result, err := namedExecContext(ctx, d.sql, upsertWorkbenchRevisionQuery, map[string]any{
		"organization_uuid": organizationUUID,
		"prompt_uuid":       strings.TrimSpace(record.PromptUUID),
		"revision_uuid":     strings.TrimSpace(record.RevisionUUID),
		"payload":           payloadJSON,
	})
	return workbenchUpsertResult(result, err)
}

func (d *DB) ListWorkbenchEvaluationRevisionIDs(ctx context.Context, orgUUID string) ([]string, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, nil
	}
	var revisionIDs []string
	err = namedSelectContext(ctx, d.sql, &revisionIDs, `
		SELECT DISTINCT revision_uuid
		FROM workbench_evaluations
		WHERE organization_uuid = :organization_uuid
		ORDER BY revision_uuid ASC
	`, map[string]any{"organization_uuid": typedOrgUUID})
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
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	var row workbenchKVRow
	err = namedGetContext(ctx, d.sql, &row, `
		SELECT k.organization_uuid,
		       k.prompt_uuid, k.key, k.value, CAST(k.version AS text) AS version_json,
		       k.created_at, k.updated_at
		FROM workbench_prompt_kv k
		JOIN workbench_prompts p ON p.uuid = k.prompt_ref_uuid
		WHERE k.organization_uuid = :organization_uuid
		  AND p.prompt_uuid = :prompt_uuid
		  AND k.key = :key
		LIMIT 1
	`, map[string]any{"organization_uuid": typedOrgUUID, "prompt_uuid": promptUUID, "key": key})
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
	organizationUUID, err := parseDBUUID("organization_uuid", record.OrgUUID)
	if err != nil {
		return platform.ErrNotFound
	}
	result, err := namedExecContext(ctx, d.sql, `
		WITH target_prompt AS (
			SELECT uuid
			FROM workbench_prompts
			WHERE organization_uuid = :organization_uuid
			  AND prompt_uuid = :prompt_uuid
			LIMIT 1
		)
		INSERT INTO workbench_prompt_kv (
			organization_uuid, prompt_ref_uuid, prompt_uuid, key, value, version,
			created_at, updated_at
		)
		SELECT :organization_uuid, target_prompt.uuid, :prompt_uuid,
		       :key, :value, CAST(:version AS jsonb), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM target_prompt
		ON CONFLICT (organization_uuid, prompt_ref_uuid, key) DO UPDATE
		SET value = EXCLUDED.value,
		    version = EXCLUDED.version,
		    updated_at = CURRENT_TIMESTAMP
	`, map[string]any{
		"organization_uuid": organizationUUID,
		"prompt_uuid":       strings.TrimSpace(record.PromptUUID),
		"key":               strings.TrimSpace(record.Key),
		"value":             record.Value,
		"version":           versionJSON,
	})
	return workbenchUpsertResult(result, err)
}

func (d *DB) DeleteWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) error {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil
	}
	_, err = namedExecContext(ctx, d.sql, `
		DELETE FROM workbench_prompt_kv k
		USING workbench_prompts p
		WHERE p.uuid = k.prompt_ref_uuid
		  AND k.organization_uuid = :organization_uuid
		  AND p.prompt_uuid = :prompt_uuid
		  AND k.key = :key
	`, map[string]any{"organization_uuid": typedOrgUUID, "prompt_uuid": promptUUID, "key": key})
	return err
}

func (d *DB) ListWorkbenchEvaluations(ctx context.Context, orgUUID string, revisionUUID string) ([]platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, nil
	}
	var rows []workbenchEvaluationRow
	err = namedSelectContext(ctx, d.sql, &rows, `
		SELECT e.organization_uuid,
		       e.revision_uuid, e.evaluation_uuid, CAST(e.payload AS text) AS payload_json,
		       e.created_at, e.updated_at
		FROM workbench_evaluations e
		JOIN workbench_prompt_revisions r ON r.uuid = e.revision_ref_uuid
		WHERE e.organization_uuid = :organization_uuid
		  AND r.revision_uuid = :revision_uuid
		ORDER BY e.created_at ASC, e.uuid ASC
	`, map[string]any{"organization_uuid": typedOrgUUID, "revision_uuid": revisionUUID})
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
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	var row workbenchEvaluationRow
	err = namedGetContext(ctx, d.sql, &row, `
		SELECT organization_uuid,
		       revision_uuid, evaluation_uuid, CAST(payload AS text) AS payload_json,
		       created_at, updated_at
		FROM workbench_evaluations
		WHERE organization_uuid = :organization_uuid
		  AND evaluation_uuid = :evaluation_uuid
		LIMIT 1
	`, map[string]any{"organization_uuid": typedOrgUUID, "evaluation_uuid": evaluationUUID})
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
	organizationUUID, err := parseDBUUID("organization_uuid", record.OrgUUID)
	if err != nil {
		return platform.ErrNotFound
	}
	result, err := namedExecContext(ctx, d.sql, `
		WITH target_revision AS (
			SELECT uuid
			FROM workbench_prompt_revisions
			WHERE organization_uuid = :organization_uuid
			  AND revision_uuid = :revision_uuid
			ORDER BY created_at DESC, uuid DESC
			LIMIT 1
		)
		INSERT INTO workbench_evaluations (
			organization_uuid, revision_ref_uuid, revision_uuid, evaluation_uuid,
			payload, created_at, updated_at
		)
		SELECT :organization_uuid, target_revision.uuid, :revision_uuid,
		       :evaluation_uuid, CAST(:payload AS jsonb), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM target_revision
		ON CONFLICT (organization_uuid, evaluation_uuid) DO UPDATE
		SET revision_ref_uuid = EXCLUDED.revision_ref_uuid,
		    revision_uuid = EXCLUDED.revision_uuid,
		    payload = EXCLUDED.payload,
		    updated_at = CURRENT_TIMESTAMP
	`, map[string]any{
		"organization_uuid": organizationUUID,
		"revision_uuid":     strings.TrimSpace(record.RevisionUUID),
		"evaluation_uuid":   strings.TrimSpace(record.EvaluationUUID),
		"payload":           payloadJSON,
	})
	return workbenchUpsertResult(result, err)
}

func (d *DB) DeleteWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	var row workbenchEvaluationRow
	err = namedGetContext(ctx, d.sql, &row, `
		DELETE FROM workbench_evaluations
		WHERE organization_uuid = :organization_uuid
		  AND evaluation_uuid = :evaluation_uuid
		RETURNING organization_uuid,
		          revision_uuid, evaluation_uuid, CAST(payload AS text) AS payload_json,
		          created_at, updated_at
	`, map[string]any{"organization_uuid": typedOrgUUID, "evaluation_uuid": evaluationUUID})
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
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	arguments := map[string]any{"organization_uuid": typedOrgUUID, "values": valuesJSON}
	if _, err := namedExecContext(ctx, tx, `
		INSERT INTO workbench_generated_test_cases (organization_uuid, values, created_at)
		VALUES (:organization_uuid, CAST(:values AS jsonb), CURRENT_TIMESTAMP)
	`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `
		DELETE FROM workbench_generated_test_cases
		WHERE organization_uuid = :organization_uuid
		  AND uuid NOT IN (
		      SELECT uuid
		      FROM workbench_generated_test_cases
		      WHERE organization_uuid = :organization_uuid
		      ORDER BY created_at DESC, uuid DESC
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
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, false, nil
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var rows []workbenchGeneratedTestCaseRow
	err = namedSelectContext(ctx, tx, &rows, `
		SELECT uuid, CAST(values AS text) AS values_json
		FROM workbench_generated_test_cases
		WHERE organization_uuid = :organization_uuid
		ORDER BY created_at ASC, uuid ASC
		FOR UPDATE
	`, map[string]any{"organization_uuid": typedOrgUUID})
	if err != nil {
		return nil, false, err
	}

	var selectedUUID uuid.UUID
	var selectedValues map[string]any
	for _, row := range rows {
		values := parseWorkbenchMapJSON(row.ValuesJSON)
		if workbenchGeneratedValuesMatchRequest(values, requested) {
			selectedUUID = row.UUID
			selectedValues = values
			break
		}
	}
	if selectedUUID == uuid.Nil {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if _, err := namedExecContext(ctx, tx, `DELETE FROM workbench_generated_test_cases WHERE uuid = :uuid`, map[string]any{
		"uuid": selectedUUID,
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
		OrgUUID:               r.OrgUUID.String(),
		PromptUUID:            r.PromptUUID,
		WorkspaceUUID:         r.WorkspaceUUID.String(),
		WorkspaceDisplayID:    r.WorkspaceDisplayID,
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
		OrgUUID:      r.OrgUUID.String(),
		PromptUUID:   r.PromptUUID,
		RevisionUUID: r.RevisionUUID,
		Payload:      parseWorkbenchMapJSON(r.PayloadJSON),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func (r workbenchKVRow) record() platform.WorkbenchKVRecord {
	record := platform.WorkbenchKVRecord{
		OrgUUID:    r.OrgUUID.String(),
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
		OrgUUID:        r.OrgUUID.String(),
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
