package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type EvaluationDataset struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	Name              string
	Description       string
	VersionUUID       string
	VersionExternalID string
	Version           int
	CaseCount         int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EvaluationCase struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	DatasetUUID       string
	VersionUUID       string
	Position          int
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	SourceSessionID   string
	Polarity          string
	Acceptance        string
	Inputs            json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EvaluationCaseWrite struct {
	UUID              string
	ExternalID        string
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	SourceSessionID   string
	Polarity          string
	Acceptance        string
	Inputs            json.RawMessage
}

func (d *DB) CreateEvaluationDataset(ctx context.Context, dataset EvaluationDataset, cases []EvaluationCaseWrite) (EvaluationDataset, error) {
	var created EvaluationDataset
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEvaluationDatasetMapper(executor)
		row, err := mapper.InsertDataset(ctx, insertEvaluationDatasetParams{
			UUID:             dataset.UUID,
			ExternalID:       dataset.ExternalID,
			OrganizationUUID: dataset.OrganizationUUID,
			WorkspaceUUID:    dataset.WorkspaceUUID,
			Name:             dataset.Name,
			Description:      dataset.Description,
			CreatedAt:        dataset.CreatedAt,
		})
		if err != nil {
			return mapEvaluationWrite(err)
		}
		version, err := mapper.InsertVersion(ctx, insertEvaluationDatasetVersionParams{
			UUID:             dataset.VersionUUID,
			ExternalID:       dataset.VersionExternalID,
			OrganizationUUID: dataset.OrganizationUUID,
			WorkspaceUUID:    dataset.WorkspaceUUID,
			DatasetUUID:      row.UUID,
			Version:          1,
			CreatedAt:        dataset.CreatedAt,
		})
		if err != nil {
			return mapEvaluationWrite(err)
		}
		for index, item := range cases {
			if _, err := mapper.InsertCase(ctx, insertCaseParams(dataset, row.UUID, version.UUID, index+1, item, dataset.CreatedAt)); err != nil {
				return mapEvaluationWrite(err)
			}
		}
		created = dataset
		created.UUID = row.UUID
		created.Version = 1
		created.CaseCount = len(cases)
		created.UpdatedAt = dataset.CreatedAt
		return nil
	})
	return created, err
}

func (d *DB) ListEvaluationDatasets(ctx context.Context, organizationUUID, workspaceUUID string, offset, limit int) ([]EvaluationDataset, error) {
	rows, err := NewEvaluationDatasetMapper(d.mapperDB).ListDatasets(ctx, evaluationListParams{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		Limit:            limit,
		Offset:           offset,
	})
	if err != nil {
		return nil, err
	}
	datasets := make([]EvaluationDataset, 0, len(rows))
	for _, row := range rows {
		datasets = append(datasets, datasetFromRow(row))
	}
	return datasets, nil
}

func (d *DB) GetEvaluationDataset(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationDataset, error) {
	row, err := NewEvaluationDatasetMapper(d.mapperDB).FindDataset(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationDataset{}, mapNoRows(err)
	}
	return datasetFromRow(row), nil
}

func (d *DB) DeleteEvaluationDataset(ctx context.Context, organizationUUID, workspaceUUID, externalID string, deletedAt time.Time) error {
	rows, err := NewEvaluationDatasetMapper(d.mapperDB).SoftDeleteDataset(ctx, organizationUUID, workspaceUUID, externalID, deletedAt)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) GetEvaluationCaseTrace(ctx context.Context, organizationUUID, workspaceUUID, versionUUID, caseExternalID string) (json.RawMessage, error) {
	row, err := NewEvaluationDatasetMapper(d.mapperDB).FindCaseTrace(ctx, organizationUUID, workspaceUUID, versionUUID, caseExternalID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return rawJSON(row.ExpectedToolTrace), nil
}

func (d *DB) ListEvaluationCases(ctx context.Context, organizationUUID, workspaceUUID, datasetExternalID string) ([]EvaluationCase, error) {
	dataset, err := d.GetEvaluationDataset(ctx, organizationUUID, workspaceUUID, datasetExternalID)
	if err != nil {
		return nil, err
	}
	rows, err := NewEvaluationDatasetMapper(d.mapperDB).ListCases(ctx, organizationUUID, workspaceUUID, dataset.VersionUUID)
	if err != nil {
		return nil, err
	}
	cases := make([]EvaluationCase, 0, len(rows))
	for _, row := range rows {
		cases = append(cases, caseFromRow(row))
	}
	return cases, nil
}

func (d *DB) CreateEvaluationCases(ctx context.Context, organizationUUID, workspaceUUID, datasetExternalID string, cases []EvaluationCaseWrite, now time.Time) ([]EvaluationCase, error) {
	var created []EvaluationCase
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEvaluationDatasetMapper(executor)
		locked, err := mapper.LockDataset(ctx, organizationUUID, workspaceUUID, datasetExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		version, err := mapper.FindLatestVersion(ctx, locked.UUID)
		if err != nil {
			return mapNoRows(err)
		}
		position, err := mapper.MaxCasePosition(ctx, version.UUID)
		if err != nil {
			return err
		}
		created = make([]EvaluationCase, 0, len(cases))
		for _, item := range cases {
			position++
			row, err := mapper.InsertCase(ctx, insertCaseParams(EvaluationDataset{
				OrganizationUUID: organizationUUID,
				WorkspaceUUID:    workspaceUUID,
			}, locked.UUID, version.UUID, position, item, now))
			if err != nil {
				return mapEvaluationWrite(err)
			}
			created = append(created, caseFromRow(row))
		}
		return nil
	})
	return created, err
}

func (d *DB) UpdateEvaluationCase(ctx context.Context, organizationUUID, workspaceUUID, datasetExternalID string, write EvaluationCaseWrite, now time.Time) (EvaluationCase, error) {
	dataset, err := d.GetEvaluationDataset(ctx, organizationUUID, workspaceUUID, datasetExternalID)
	if err != nil {
		return EvaluationCase{}, err
	}
	row, err := NewEvaluationDatasetMapper(d.mapperDB).UpdateCase(ctx, updateEvaluationCaseParams{
		OrganizationUUID:  organizationUUID,
		WorkspaceUUID:     workspaceUUID,
		DatasetUUID:       dataset.UUID,
		ExternalID:        write.ExternalID,
		Query:             write.Query,
		ExpectedAnswer:    write.ExpectedAnswer,
		ExpectedToolTrace: write.ExpectedToolTrace,
		SourceSessionID:   write.SourceSessionID,
		Polarity:          write.Polarity,
		Acceptance:        write.Acceptance,
		Inputs:            write.Inputs,
		UpdatedAt:         now,
	})
	if err != nil {
		return EvaluationCase{}, mapNoRows(err)
	}
	return caseFromRow(row), nil
}

func (d *DB) DeleteEvaluationCase(ctx context.Context, organizationUUID, workspaceUUID, datasetExternalID, caseExternalID string, deletedAt time.Time) error {
	dataset, err := d.GetEvaluationDataset(ctx, organizationUUID, workspaceUUID, datasetExternalID)
	if err != nil {
		return err
	}
	rows, err := NewEvaluationDatasetMapper(d.mapperDB).SoftDeleteCase(ctx, organizationUUID, workspaceUUID, dataset.UUID, caseExternalID, deletedAt)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func insertCaseParams(dataset EvaluationDataset, datasetUUID, versionUUID string, position int, item EvaluationCaseWrite, now time.Time) insertEvaluationCaseParams {
	return insertEvaluationCaseParams{
		UUID:              item.UUID,
		ExternalID:        item.ExternalID,
		OrganizationUUID:  dataset.OrganizationUUID,
		WorkspaceUUID:     dataset.WorkspaceUUID,
		DatasetUUID:       datasetUUID,
		VersionUUID:       versionUUID,
		Position:          position,
		Query:             item.Query,
		ExpectedAnswer:    item.ExpectedAnswer,
		ExpectedToolTrace: item.ExpectedToolTrace,
		SourceSessionID:   item.SourceSessionID,
		Polarity:          item.Polarity,
		Acceptance:        item.Acceptance,
		Inputs:            item.Inputs,
		CreatedAt:         now,
	}
}

func datasetFromRow(row evaluationDatasetRow) EvaluationDataset {
	return EvaluationDataset{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		Name:              row.Name,
		Description:       row.Description,
		VersionUUID:       row.VersionUUID,
		VersionExternalID: row.VersionExternalID,
		Version:           row.Version,
		CaseCount:         row.CaseCount,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func caseFromRow(row evaluationCaseRow) EvaluationCase {
	return EvaluationCase{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		DatasetUUID:       row.DatasetUUID,
		VersionUUID:       row.VersionUUID,
		Position:          row.Position,
		Query:             row.Query,
		ExpectedAnswer:    row.ExpectedAnswer,
		ExpectedToolTrace: rawJSON(row.ExpectedToolTrace),
		SourceSessionID:   row.SourceSessionID,
		Polarity:          row.Polarity,
		Acceptance:        row.Acceptance,
		Inputs:            rawJSON(row.Inputs),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(value)
}

func mapEvaluationWrite(err error) error {
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}
