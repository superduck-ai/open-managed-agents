package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"
)

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return platform.ErrNotFound
	}
	return err
}

func workbenchUpsertResult(rowsAffected int64, err error) error {
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return platform.ErrNotFound
	}
	return nil
}

func (d *DB) GetWorkbenchPrompt(ctx context.Context, orgUUID string, promptUUID string) (*platform.WorkbenchPromptRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	mapper := NewWorkbenchPromptMapper(d.mapperDB)
	row, err := mapper.FindByPromptUUID(ctx, typedOrgUUID.String(), promptUUID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) ListWorkbenchPrompts(ctx context.Context, orgUUID string, workspaceUUID string) ([]platform.WorkbenchPromptRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceUUID) == "" {
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
	mapper := NewWorkbenchPromptMapper(d.mapperDB)
	rows, err := mapper.ListByWorkspace(ctx, typedOrgUUID.String(), typedWorkspaceUUID.String())
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
	if d == nil || d.mapperDB == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" {
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
	var latestRevisionUUID *string
	if record.LatestRevisionUUID != nil && strings.TrimSpace(*record.LatestRevisionUUID) != "" {
		value := strings.TrimSpace(*record.LatestRevisionUUID)
		latestRevisionUUID = &value
	}
	var deletedAt *time.Time
	if record.DeletedAt != nil && !record.DeletedAt.IsZero() {
		value := record.DeletedAt.UTC()
		deletedAt = &value
	}
	mapper := NewWorkbenchPromptMapper(d.mapperDB)
	row, err := mapper.Upsert(ctx, upsertWorkbenchPromptParams{
		OrganizationUUID:      organizationUUID.String(),
		PromptUUID:            record.PromptUUID,
		WorkspaceUUID:         workspaceUUID.String(),
		WorkspaceDisplayID:    record.WorkspaceDisplayID,
		Name:                  record.Name,
		IsSharedWithWorkspace: record.IsSharedWithWorkspace,
		LatestRevisionUUID:    latestRevisionUUID,
		DeletedAt:             deletedAt,
		CreatedAt:             record.CreatedAt,
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
	if d == nil || d.mapperDB == nil ||
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
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		promptMapper := NewWorkbenchPromptMapper(executor)
		revisionMapper := NewWorkbenchPromptRevisionMapper(executor)
		kvMapper := NewWorkbenchPromptKVMapper(executor)
		evaluationMapper := NewWorkbenchEvaluationMapper(executor)
		generatedTestCaseMapper := NewWorkbenchGeneratedTestCaseMapper(executor)

		promptRefUUID, txErr := promptMapper.ResetAndReturnUUID(ctx, resetWorkbenchPromptParams{
			OrganizationUUID: organizationUUID.String(),
			PromptUUID:       promptUUID,
			WorkspaceUUID:    typedWorkspaceUUID.String(),
			WorkspaceDisplayID: firstNonEmpty(
				strings.TrimSpace(workspaceDisplayID),
				strings.TrimSpace(workspaceUUID),
			),
		})
		if txErr != nil {
			return txErr
		}
		if txErr = revisionMapper.DeleteByPromptRefUUID(ctx, promptRefUUID); txErr != nil {
			return txErr
		}
		if txErr = kvMapper.DeleteByPromptRefUUID(ctx, promptRefUUID); txErr != nil {
			return txErr
		}
		if txErr = evaluationMapper.DeleteByOrganization(ctx, organizationUUID.String()); txErr != nil {
			return txErr
		}
		return generatedTestCaseMapper.DeleteByOrganization(ctx, organizationUUID.String())
	})
}

func (d *DB) GetWorkbenchRevision(ctx context.Context, orgUUID string, promptUUID string, revisionUUID string) (*platform.WorkbenchRevisionRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	mapper := NewWorkbenchPromptRevisionMapper(d.mapperDB)
	row, err := mapper.Find(ctx, typedOrgUUID.String(), promptUUID, revisionUUID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) UpsertWorkbenchRevision(ctx context.Context, record platform.WorkbenchRevisionRecord) error {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" || strings.TrimSpace(record.RevisionUUID) == "" {
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
	mapper := NewWorkbenchPromptRevisionMapper(d.mapperDB)
	rowsAffected, err := mapper.Upsert(ctx, upsertWorkbenchRevisionParams{
		OrganizationUUID: organizationUUID.String(),
		PromptUUID:       strings.TrimSpace(record.PromptUUID),
		RevisionUUID:     strings.TrimSpace(record.RevisionUUID),
		PayloadJSON:      payloadJSON,
	})
	return workbenchUpsertResult(rowsAffected, err)
}

func (d *DB) ListWorkbenchEvaluationRevisionIDs(ctx context.Context, orgUUID string) ([]string, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" {
		return nil, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, nil
	}
	mapper := NewWorkbenchEvaluationMapper(d.mapperDB)
	rows, err := mapper.ListRevisionIDs(ctx, typedOrgUUID.String())
	if err != nil {
		return nil, err
	}
	revisionIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.RevisionUUID) != "" {
			revisionIDs = append(revisionIDs, row.RevisionUUID)
		}
	}
	return revisionIDs, nil
}

func (d *DB) GetWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) (*platform.WorkbenchKVRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	mapper := NewWorkbenchPromptKVMapper(d.mapperDB)
	row, err := mapper.Find(ctx, typedOrgUUID.String(), promptUUID, key)
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) UpsertWorkbenchKV(ctx context.Context, record platform.WorkbenchKVRecord) error {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.PromptUUID) == "" || strings.TrimSpace(record.Key) == "" {
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
	mapper := NewWorkbenchPromptKVMapper(d.mapperDB)
	rowsAffected, err := mapper.Upsert(ctx, upsertWorkbenchKVParams{
		OrganizationUUID: organizationUUID.String(),
		PromptUUID:       strings.TrimSpace(record.PromptUUID),
		Key:              strings.TrimSpace(record.Key),
		Value:            record.Value,
		VersionJSON:      versionJSON,
	})
	return workbenchUpsertResult(rowsAffected, err)
}

func (d *DB) DeleteWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) error {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(promptUUID) == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil
	}
	mapper := NewWorkbenchPromptKVMapper(d.mapperDB)
	return mapper.Delete(ctx, typedOrgUUID.String(), promptUUID, key)
}

func (d *DB) ListWorkbenchEvaluations(ctx context.Context, orgUUID string, revisionUUID string) ([]platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(revisionUUID) == "" {
		return nil, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, nil
	}
	mapper := NewWorkbenchEvaluationMapper(d.mapperDB)
	rows, err := mapper.ListByRevision(ctx, typedOrgUUID.String(), revisionUUID)
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
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	mapper := NewWorkbenchEvaluationMapper(d.mapperDB)
	row, err := mapper.Find(ctx, typedOrgUUID.String(), evaluationUUID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) UpsertWorkbenchEvaluation(ctx context.Context, record platform.WorkbenchEvaluationRecord) error {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(record.OrgUUID) == "" || strings.TrimSpace(record.RevisionUUID) == "" || strings.TrimSpace(record.EvaluationUUID) == "" {
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
	mapper := NewWorkbenchEvaluationMapper(d.mapperDB)
	rowsAffected, err := mapper.Upsert(ctx, upsertWorkbenchEvaluationParams{
		OrganizationUUID: organizationUUID.String(),
		RevisionUUID:     strings.TrimSpace(record.RevisionUUID),
		EvaluationUUID:   strings.TrimSpace(record.EvaluationUUID),
		PayloadJSON:      payloadJSON,
	})
	return workbenchUpsertResult(rowsAffected, err)
}

func (d *DB) DeleteWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*platform.WorkbenchEvaluationRecord, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(evaluationUUID) == "" {
		return nil, platform.ErrNotFound
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, platform.ErrNotFound
	}
	mapper := NewWorkbenchEvaluationMapper(d.mapperDB)
	row, err := mapper.Delete(ctx, typedOrgUUID.String(), evaluationUUID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	record := row.record()
	return &record, nil
}

func (d *DB) AppendWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, values map[string]any) error {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || len(values) == 0 {
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
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewWorkbenchGeneratedTestCaseMapper(executor)
		if txErr := mapper.Insert(ctx, typedOrgUUID.String(), valuesJSON); txErr != nil {
			return txErr
		}
		return mapper.DeleteOlderThanLimit(ctx, typedOrgUUID.String(), 10)
	})
}

func (d *DB) TakeWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, requested map[string]any) (map[string]any, bool, error) {
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" || len(requested) == 0 {
		return nil, false, nil
	}
	typedOrgUUID, err := parseDBUUID("organization_uuid", orgUUID)
	if err != nil {
		return nil, false, nil
	}
	var selectedValues map[string]any
	var found bool
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewWorkbenchGeneratedTestCaseMapper(executor)
		rows, txErr := mapper.ListForUpdate(ctx, typedOrgUUID.String())
		if txErr != nil {
			return txErr
		}
		selectedUUID := ""
		for _, row := range rows {
			values := parseWorkbenchMapJSON(row.ValuesJSON)
			if workbenchGeneratedValuesMatchRequest(values, requested) {
				selectedUUID = row.UUID
				selectedValues = values
				break
			}
		}
		if selectedUUID == "" {
			return nil
		}
		if txErr = mapper.DeleteByUUID(ctx, selectedUUID); txErr != nil {
			return txErr
		}
		found = true
		return nil
	})
	return selectedValues, found, err
}

func (r workbenchPromptRow) record() platform.WorkbenchPromptRecord {
	record := platform.WorkbenchPromptRecord{
		OrgUUID:               r.OrgUUID,
		PromptUUID:            r.PromptUUID,
		WorkspaceUUID:         r.WorkspaceUUID,
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

func marshalWorkbenchNullableJSON(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := string(encoded)
	return &result, nil
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
