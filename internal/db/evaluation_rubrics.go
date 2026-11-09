package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type EvaluationRubric struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	Name              string
	Description       string
	VersionUUID       string
	VersionExternalID string
	Version           int
	Body              string
	PassScore         int
	Model             string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (d *DB) CreateEvaluationRubric(ctx context.Context, rubric EvaluationRubric) (EvaluationRubric, error) {
	var created EvaluationRubric
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEvaluationRubricMapper(executor)
		row, err := mapper.InsertRubric(ctx, insertEvaluationRubricParams{
			UUID:             rubric.UUID,
			ExternalID:       rubric.ExternalID,
			OrganizationUUID: rubric.OrganizationUUID,
			WorkspaceUUID:    rubric.WorkspaceUUID,
			Name:             rubric.Name,
			Description:      rubric.Description,
			CreatedAt:        rubric.CreatedAt,
		})
		if err != nil {
			return mapEvaluationWrite(err)
		}
		version, err := mapper.InsertRubricVersion(ctx, insertEvaluationRubricVersionParams{
			UUID:             rubric.VersionUUID,
			ExternalID:       rubric.VersionExternalID,
			OrganizationUUID: rubric.OrganizationUUID,
			WorkspaceUUID:    rubric.WorkspaceUUID,
			RubricUUID:       row.UUID,
			Version:          1,
			Body:             rubric.Body,
			PassScore:        rubric.PassScore,
			Model:            rubric.Model,
			CreatedAt:        rubric.CreatedAt,
		})
		if err != nil {
			return mapEvaluationWrite(err)
		}
		created = rubric
		created.UUID = row.UUID
		created.VersionUUID = version.UUID
		created.Version = 1
		created.UpdatedAt = rubric.CreatedAt
		return nil
	})
	return created, err
}

func (d *DB) AppendEvaluationRubricVersion(ctx context.Context, organizationUUID, workspaceUUID, externalID string, version EvaluationRubric, now time.Time) (EvaluationRubric, error) {
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEvaluationRubricMapper(executor)
		locked, err := mapper.LockRubric(ctx, organizationUUID, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		current, err := mapper.FindLatestRubricVersion(ctx, locked.UUID)
		if err != nil {
			return mapNoRows(err)
		}
		if _, err := mapper.InsertRubricVersion(ctx, insertEvaluationRubricVersionParams{
			UUID:             version.VersionUUID,
			ExternalID:       version.VersionExternalID,
			OrganizationUUID: organizationUUID,
			WorkspaceUUID:    workspaceUUID,
			RubricUUID:       locked.UUID,
			Version:          current.Version + 1,
			Body:             version.Body,
			PassScore:        version.PassScore,
			Model:            version.Model,
			CreatedAt:        now,
		}); err != nil {
			return mapEvaluationWrite(err)
		}
		if _, err := mapper.TouchRubric(ctx, locked.UUID, now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return EvaluationRubric{}, err
	}
	return d.GetEvaluationRubric(ctx, organizationUUID, workspaceUUID, externalID)
}

func (d *DB) ListEvaluationRubrics(ctx context.Context, organizationUUID, workspaceUUID string, offset, limit int) ([]EvaluationRubric, error) {
	rows, err := NewEvaluationRubricMapper(d.mapperDB).ListRubrics(ctx, evaluationListParams{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	rubrics := make([]EvaluationRubric, 0, len(rows))
	for _, row := range rows {
		rubrics = append(rubrics, rubricFromRow(row))
	}
	return rubrics, nil
}

func (d *DB) GetEvaluationRubric(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationRubric, error) {
	row, err := NewEvaluationRubricMapper(d.mapperDB).FindRubric(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationRubric{}, mapNoRows(err)
	}
	return rubricFromRow(row), nil
}

func (d *DB) DeleteEvaluationRubric(ctx context.Context, organizationUUID, workspaceUUID, externalID string, deletedAt time.Time) error {
	rows, err := NewEvaluationRubricMapper(d.mapperDB).SoftDeleteRubric(ctx, organizationUUID, workspaceUUID, externalID, deletedAt)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func rubricFromRow(row evaluationRubricRow) EvaluationRubric {
	return EvaluationRubric{
		UUID: row.UUID, ExternalID: row.ExternalID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID,
		Name: row.Name, Description: row.Description, VersionUUID: row.VersionUUID, VersionExternalID: row.VersionExternalID,
		Version: row.Version, Body: row.Body, PassScore: row.PassScore, Model: row.Model, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
