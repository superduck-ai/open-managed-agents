package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type EvaluationDatasetVersion struct {
	UUID        string
	ExternalID  string
	DatasetUUID string
	Version     int
	CaseCount   int
	CreatedAt   time.Time
}

type EvaluationDataset struct {
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
	Version           EvaluationDatasetVersion
}

type EvaluationDatasetPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListEvaluationDatasetsPageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Limit            int
	Cursor           *EvaluationDatasetPageCursor
}

func (d *DB) CreateEvaluationDataset(ctx context.Context, dataset EvaluationDataset) (EvaluationDataset, error) {
	var created EvaluationDataset
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, insertErr := NewEvaluationDatasetMapper(executor).Insert(ctx, evaluationDatasetWriteParams{
			UUID:              dataset.UUID,
			ExternalID:        dataset.ExternalID,
			OrganizationUUID:  dataset.OrganizationUUID,
			WorkspaceUUID:     dataset.WorkspaceUUID,
			Name:              dataset.Name,
			Description:       dataset.Description,
			CreatedByUserUUID: nullableString(dataset.CreatedByUserUUID),
			CreatedAt:         dataset.CreatedAt,
		})
		if insertErr != nil {
			return insertErr
		}
		version, versionErr := NewEvaluationDatasetVersionMapper(executor).Insert(ctx, evaluationDatasetVersionWriteParams{
			UUID:             dataset.Version.UUID,
			ExternalID:       dataset.Version.ExternalID,
			OrganizationUUID: dataset.OrganizationUUID,
			WorkspaceUUID:    dataset.WorkspaceUUID,
			DatasetUUID:      dataset.UUID,
			Version:          dataset.Version.Version,
			CreatedAt:        dataset.Version.CreatedAt,
		})
		if versionErr != nil {
			return versionErr
		}
		created = row.evaluationDataset()
		created.Version = version.evaluationDatasetVersion()
		return nil
	})
	if isUniqueViolation(err) {
		return EvaluationDataset{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetEvaluationDataset(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationDataset, error) {
	row, err := NewEvaluationDatasetMapper(d.mapperDB).FindByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationDataset{}, mapNoRows(err)
	}
	return row.evaluationDataset(), nil
}

func (d *DB) ListEvaluationDatasetsPage(ctx context.Context, params ListEvaluationDatasetsPageParams) ([]EvaluationDataset, bool, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, err := NewEvaluationDatasetMapper(d.mapperDB).ListPage(ctx, evaluationDatasetPageParams{
		OrganizationUUID: params.OrganizationUUID,
		WorkspaceUUID:    params.WorkspaceUUID,
		FetchLimit:       limit + 1,
		Cursor:           params.Cursor,
	})
	if err != nil {
		return nil, false, err
	}
	datasets := make([]EvaluationDataset, 0, len(rows))
	for _, row := range rows {
		datasets = append(datasets, row.evaluationDataset())
	}
	hasMore := len(datasets) > limit
	if hasMore {
		datasets = datasets[:limit]
	}
	return datasets, hasMore, nil
}

func (row evaluationDatasetRow) evaluationDataset() EvaluationDataset {
	return EvaluationDataset{
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

func (row evaluationDatasetPageRow) evaluationDataset() EvaluationDataset {
	dataset := evaluationDatasetRow{
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
	}.evaluationDataset()
	dataset.Version = EvaluationDatasetVersion{
		UUID:        row.VersionUUID,
		ExternalID:  row.VersionExternalID,
		DatasetUUID: row.UUID,
		Version:     row.VersionNumber,
		CaseCount:   int(row.CaseCount),
		CreatedAt:   row.VersionCreatedAt,
	}
	return dataset
}

func (row evaluationDatasetVersionRow) evaluationDatasetVersion() EvaluationDatasetVersion {
	return EvaluationDatasetVersion{
		UUID:        row.UUID,
		ExternalID:  row.ExternalID,
		DatasetUUID: row.DatasetUUID,
		Version:     row.Version,
		CreatedAt:   row.CreatedAt,
	}
}

func lockEvaluationDataset(ctx context.Context, executor yourbatis.Executor, organizationUUID, workspaceUUID, externalID string) (evaluationDatasetRow, error) {
	row, err := NewEvaluationDatasetMapper(executor).FindForUpdate(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return evaluationDatasetRow{}, mapNoRows(err)
	}
	return row, nil
}
