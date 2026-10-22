package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type EvaluationEvaluatorVersion struct {
	UUID           string
	ExternalID     string
	EvaluatorUUID  string
	Version        int
	EvaluatorType  string
	Model          string
	AgentID        string
	PromptTemplate string
	Variables      []string
	CreatedAt      time.Time
}

type EvaluationEvaluator struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	Name              string
	Description       string
	CreatedByUserUUID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	Version           EvaluationEvaluatorVersion
}

type EvaluationEvaluatorPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListEvaluationEvaluatorsPageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Limit            int
	Cursor           *EvaluationEvaluatorPageCursor
}

func (d *DB) CreateEvaluationEvaluator(ctx context.Context, evaluator EvaluationEvaluator) (EvaluationEvaluator, error) {
	variablesJSON, err := encodeEvaluatorVariables(evaluator.Version.Variables)
	if err != nil {
		return EvaluationEvaluator{}, err
	}
	var created EvaluationEvaluator
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, insertErr := NewEvaluationEvaluatorMapper(executor).Insert(ctx, evaluationEvaluatorWriteParams{
			UUID:              evaluator.UUID,
			ExternalID:        evaluator.ExternalID,
			OrganizationUUID:  evaluator.OrganizationUUID,
			WorkspaceUUID:     evaluator.WorkspaceUUID,
			Name:              evaluator.Name,
			Description:       evaluator.Description,
			CreatedByUserUUID: nullableString(evaluator.CreatedByUserUUID),
			CreatedAt:         evaluator.CreatedAt,
		})
		if insertErr != nil {
			return insertErr
		}
		version, versionErr := NewEvaluationEvaluatorVersionMapper(executor).Insert(ctx, evaluationEvaluatorVersionWriteParams{
			UUID:             evaluator.Version.UUID,
			ExternalID:       evaluator.Version.ExternalID,
			OrganizationUUID: evaluator.OrganizationUUID,
			WorkspaceUUID:    evaluator.WorkspaceUUID,
			EvaluatorUUID:    evaluator.UUID,
			Version:          evaluator.Version.Version,
			EvaluatorType:    evaluator.Version.EvaluatorType,
			Model:            evaluator.Version.Model,
			AgentID:          evaluator.Version.AgentID,
			PromptTemplate:   evaluator.Version.PromptTemplate,
			VariablesJSON:    variablesJSON,
			CreatedAt:        evaluator.Version.CreatedAt,
		})
		if versionErr != nil {
			return versionErr
		}
		created = row.evaluationEvaluator()
		decoded, decodeErr := version.evaluationEvaluatorVersion()
		if decodeErr != nil {
			return decodeErr
		}
		created.Version = decoded
		return nil
	})
	if isUniqueViolation(err) {
		return EvaluationEvaluator{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) AppendEvaluationEvaluatorVersion(ctx context.Context, organizationUUID, workspaceUUID, externalID string, version EvaluationEvaluatorVersion) (EvaluationEvaluator, error) {
	variablesJSON, err := encodeEvaluatorVariables(version.Variables)
	if err != nil {
		return EvaluationEvaluator{}, err
	}
	var created EvaluationEvaluator
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, lockErr := NewEvaluationEvaluatorMapper(executor).FindForUpdate(ctx, organizationUUID, workspaceUUID, externalID)
		if lockErr != nil {
			return mapNoRows(lockErr)
		}
		latest, latestErr := NewEvaluationEvaluatorVersionMapper(executor).FindLatest(ctx, organizationUUID, workspaceUUID, row.UUID)
		if latestErr != nil {
			return mapNoRows(latestErr)
		}
		if latest.EvaluatorType != version.EvaluatorType {
			return fmt.Errorf("evaluation evaluator type is %s", latest.EvaluatorType)
		}
		inserted, insertErr := NewEvaluationEvaluatorVersionMapper(executor).Insert(ctx, evaluationEvaluatorVersionWriteParams{
			UUID:             version.UUID,
			ExternalID:       version.ExternalID,
			OrganizationUUID: organizationUUID,
			WorkspaceUUID:    workspaceUUID,
			EvaluatorUUID:    row.UUID,
			Version:          latest.Version + 1,
			EvaluatorType:    latest.EvaluatorType,
			Model:            version.Model,
			AgentID:          version.AgentID,
			PromptTemplate:   version.PromptTemplate,
			VariablesJSON:    variablesJSON,
			CreatedAt:        version.CreatedAt,
		})
		if insertErr != nil {
			return insertErr
		}
		created = row.evaluationEvaluator()
		decoded, decodeErr := inserted.evaluationEvaluatorVersion()
		if decodeErr != nil {
			return decodeErr
		}
		created.Version = decoded
		return nil
	})
	if isUniqueViolation(err) {
		return EvaluationEvaluator{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetEvaluationEvaluatorVersion(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationEvaluatorVersion, error) {
	row, err := NewEvaluationEvaluatorVersionMapper(d.mapperDB).FindScopedByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationEvaluatorVersion{}, mapNoRows(err)
	}
	return row.evaluationEvaluatorVersion()
}

func (d *DB) GetEvaluationEvaluator(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationEvaluator, error) {
	row, err := NewEvaluationEvaluatorMapper(d.mapperDB).FindByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationEvaluator{}, mapNoRows(err)
	}
	return row.evaluationEvaluator()
}

func (d *DB) ListEvaluationEvaluatorsPage(ctx context.Context, params ListEvaluationEvaluatorsPageParams) ([]EvaluationEvaluator, bool, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, err := NewEvaluationEvaluatorMapper(d.mapperDB).ListPage(ctx, evaluationEvaluatorPageParams{
		OrganizationUUID: params.OrganizationUUID,
		WorkspaceUUID:    params.WorkspaceUUID,
		FetchLimit:       limit + 1,
		Cursor:           params.Cursor,
	})
	if err != nil {
		return nil, false, err
	}
	evaluators := make([]EvaluationEvaluator, 0, len(rows))
	for _, row := range rows {
		item, decodeErr := row.evaluationEvaluator()
		if decodeErr != nil {
			return nil, false, decodeErr
		}
		evaluators = append(evaluators, item)
	}
	hasMore := len(evaluators) > limit
	if hasMore {
		evaluators = evaluators[:limit]
	}
	return evaluators, hasMore, nil
}

func (row evaluationEvaluatorRow) evaluationEvaluator() EvaluationEvaluator {
	return EvaluationEvaluator{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		Name:              row.Name,
		Description:       row.Description,
		CreatedByUserUUID: stringFromNullable(row.CreatedByUserUUID),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		ArchivedAt:        row.ArchivedAt,
	}
}

func (row evaluationEvaluatorPageRow) evaluationEvaluator() (EvaluationEvaluator, error) {
	variables, err := decodeEvaluatorVariables(row.VariablesJSON)
	if err != nil {
		return EvaluationEvaluator{}, err
	}
	evaluator := evaluationEvaluatorRow{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		Name:              row.Name,
		Description:       row.Description,
		CreatedByUserUUID: row.CreatedByUserUUID,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		ArchivedAt:        row.ArchivedAt,
	}.evaluationEvaluator()
	evaluator.Version = EvaluationEvaluatorVersion{
		UUID:           row.VersionUUID,
		ExternalID:     row.VersionExternalID,
		EvaluatorUUID:  row.UUID,
		Version:        row.VersionNumber,
		EvaluatorType:  row.EvaluatorType,
		Model:          row.Model,
		AgentID:        row.AgentID,
		PromptTemplate: row.PromptTemplate,
		Variables:      variables,
		CreatedAt:      row.VersionCreatedAt,
	}
	return evaluator, nil
}

func (row evaluationEvaluatorVersionRow) evaluationEvaluatorVersion() (EvaluationEvaluatorVersion, error) {
	variables, err := decodeEvaluatorVariables(row.VariablesJSON)
	if err != nil {
		return EvaluationEvaluatorVersion{}, err
	}
	return EvaluationEvaluatorVersion{
		UUID:           row.UUID,
		ExternalID:     row.ExternalID,
		EvaluatorUUID:  row.EvaluatorUUID,
		Version:        row.Version,
		EvaluatorType:  row.EvaluatorType,
		Model:          row.Model,
		AgentID:        row.AgentID,
		PromptTemplate: row.PromptTemplate,
		Variables:      variables,
		CreatedAt:      row.CreatedAt,
	}, nil
}

func encodeEvaluatorVariables(variables []string) (string, error) {
	if variables == nil {
		variables = []string{}
	}
	payload, err := json.Marshal(variables)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeEvaluatorVariables(raw string) ([]string, error) {
	var variables []string
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil, err
	}
	if variables == nil {
		variables = []string{}
	}
	return variables, nil
}
