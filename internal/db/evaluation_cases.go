package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type EvaluationCase struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	DatasetUUID       string
	DatasetExternalID string
	VersionUUID       string
	VersionExternalID string
	Position          int
	Name              string
	Description       string
	Turns             []string
	Checkpoints       json.RawMessage
	InputFiles        json.RawMessage
	ReferenceFiles    json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EvaluationCaseWrite struct {
	UUID           string
	ExternalID     string
	Name           string
	Description    string
	Turns          []string
	Checkpoints    json.RawMessage
	InputFiles     json.RawMessage
	ReferenceFiles json.RawMessage
}

type EvaluationCasePageCursor struct {
	Position int
	UUID     string
}

type ListEvaluationCasesPageParams struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	DatasetExternalID string
	Limit             int
	Cursor            *EvaluationCasePageCursor
}

func (d *DB) ListEvaluationCasesPage(ctx context.Context, params ListEvaluationCasesPageParams) ([]EvaluationCase, bool, error) {
	dataset, err := d.GetEvaluationDataset(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.DatasetExternalID)
	if err != nil {
		return nil, false, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, err := NewEvaluationCaseMapper(d.mapperDB).ListPage(ctx, evaluationCasePageParams{
		OrganizationUUID: params.OrganizationUUID,
		WorkspaceUUID:    params.WorkspaceUUID,
		VersionUUID:      dataset.Version.UUID,
		FetchLimit:       limit + 1,
		Cursor:           params.Cursor,
	})
	if err != nil {
		return nil, false, err
	}
	cases := make([]EvaluationCase, 0, len(rows))
	for _, row := range rows {
		item, decodeErr := row.evaluationCase(dataset.ExternalID, dataset.Version.ExternalID)
		if decodeErr != nil {
			return nil, false, decodeErr
		}
		cases = append(cases, item)
	}
	hasMore := len(cases) > limit
	if hasMore {
		cases = cases[:limit]
	}
	return cases, hasMore, nil
}

func (d *DB) CreateEvaluationCases(ctx context.Context, organizationUUID, workspaceUUID, datasetExternalID string, writes []EvaluationCaseWrite, now time.Time) ([]EvaluationCase, error) {
	if len(writes) == 0 {
		return nil, ErrNotFound
	}
	var created []EvaluationCase
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		dataset, lockErr := lockEvaluationDataset(ctx, executor, organizationUUID, workspaceUUID, datasetExternalID)
		if lockErr != nil {
			return lockErr
		}
		version, versionErr := NewEvaluationDatasetVersionMapper(executor).FindLatest(ctx, organizationUUID, workspaceUUID, dataset.UUID)
		if versionErr != nil {
			return mapNoRows(versionErr)
		}
		cases := NewEvaluationCaseMapper(executor)
		position, positionErr := cases.MaxPosition(ctx, organizationUUID, workspaceUUID, version.UUID)
		if positionErr != nil {
			return positionErr
		}
		created = make([]EvaluationCase, 0, len(writes))
		for _, write := range writes {
			position++
			turnsJSON, encodeErr := encodeEvaluationTurns(write.Turns)
			if encodeErr != nil {
				return encodeErr
			}
			row, insertErr := cases.Insert(ctx, evaluationCaseWriteParams{
				UUID:               write.UUID,
				ExternalID:         write.ExternalID,
				OrganizationUUID:   organizationUUID,
				WorkspaceUUID:      workspaceUUID,
				DatasetUUID:        dataset.UUID,
				VersionUUID:        version.UUID,
				Position:           position,
				Name:               write.Name,
				Description:        write.Description,
				TurnsJSON:          turnsJSON,
				CheckpointsJSON:    encodeOptionalJSON(write.Checkpoints),
				InputFilesJSON:     encodeOptionalJSON(write.InputFiles),
				ReferenceFilesJSON: encodeOptionalJSON(write.ReferenceFiles),
				CreatedAt:          now,
			})
			if insertErr != nil {
				return insertErr
			}
			item, decodeErr := row.evaluationCase(dataset.ExternalID, version.ExternalID)
			if decodeErr != nil {
				return decodeErr
			}
			created = append(created, item)
		}
		return nil
	})
	if isUniqueViolation(err) {
		return nil, ErrDuplicate
	}
	return created, err
}

func (row evaluationCaseRow) evaluationCase(datasetExternalID, versionExternalID string) (EvaluationCase, error) {
	turns, err := decodeEvaluationTurns(row.TurnsJSON)
	if err != nil {
		return EvaluationCase{}, err
	}
	return EvaluationCase{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		DatasetUUID:       row.DatasetUUID,
		DatasetExternalID: datasetExternalID,
		VersionUUID:       row.VersionUUID,
		VersionExternalID: versionExternalID,
		Position:          row.Position,
		Name:              row.Name,
		Description:       row.Description,
		Turns:             turns,
		Checkpoints:       decodeOptionalJSON(row.CheckpointsJSON),
		InputFiles:        decodeOptionalJSON(row.InputFilesJSON),
		ReferenceFiles:    decodeOptionalJSON(row.ReferenceFilesJSON),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}, nil
}

func encodeEvaluationTurns(turns []string) (string, error) {
	payload, err := json.Marshal(turns)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeEvaluationTurns(raw string) ([]string, error) {
	var turns []string
	if err := json.Unmarshal([]byte(raw), &turns); err != nil {
		return nil, err
	}
	if turns == nil {
		turns = []string{}
	}
	return turns, nil
}

func encodeOptionalJSON(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	encoded := string(raw)
	return &encoded
}

func decodeOptionalJSON(raw *string) json.RawMessage {
	if raw == nil || *raw == "" || *raw == "null" {
		return nil
	}
	return json.RawMessage(*raw)
}
