package db

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/superduck-ai/yourbatis"
)

const (
	EvaluationExperimentStatusPending   = "pending"
	EvaluationExperimentStatusRunning   = "running"
	EvaluationExperimentStatusCompleted = "completed"
	EvaluationExperimentStatusFailed    = "failed"
	EvaluationExperimentStatusCancelled = "cancelled"
)

type EvaluationExperimentBinding struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	ExperimentUUID      string
	EvaluatorUUID       string
	EvaluatorExternalID string
	VersionUUID         string
	VersionExternalID   string
	Position            int
	CreatedAt           time.Time
}

type EvaluationExperimentItem struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	ExperimentUUID    string
	CaseUUID          string
	CaseExternalID    string
	Position          int
	SessionExternalID string
	Status            string
	ErrorMessage      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EvaluationExperiment struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	Name                  string
	Description           string
	DatasetUUID           string
	DatasetExternalID     string
	VersionUUID           string
	VersionExternalID     string
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	TimeoutSeconds        int
	Env                   json.RawMessage
	Resources             json.RawMessage
	EvaluatorSetKey       string
	Status                string
	CreatedByUserUUID     string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ItemCount             int64
	SessionCount          int64
	EvaluatorCount        int64
	Bindings              []EvaluationExperimentBinding
	Items                 []EvaluationExperimentItem
}

type EvaluationExperimentCreate struct {
	Experiment EvaluationExperiment
	Bindings   []EvaluationExperimentBinding
	Items      []EvaluationExperimentItem
}

type EvaluationExperimentPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListEvaluationExperimentsPageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Limit            int
	Cursor           *EvaluationExperimentPageCursor
}

type EvaluationExperimentItemSession struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	ExternalID        string
	SessionExternalID string
	Status            string
	ErrorMessage      string
	UpdatedAt         time.Time
}

func EvaluationEvaluatorSetKey(versionExternalIDs []string) string {
	ordered := append([]string(nil), versionExternalIDs...)
	sort.Strings(ordered)
	return strings.Join(ordered, "|")
}

func (d *DB) CreateEvaluationExperiment(ctx context.Context, input EvaluationExperimentCreate) (EvaluationExperiment, error) {
	var created EvaluationExperiment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, insertErr := NewEvaluationExperimentMapper(executor).Insert(ctx, evaluationExperimentWriteParams{
			UUID:                  input.Experiment.UUID,
			ExternalID:            input.Experiment.ExternalID,
			OrganizationUUID:      input.Experiment.OrganizationUUID,
			WorkspaceUUID:         input.Experiment.WorkspaceUUID,
			Name:                  input.Experiment.Name,
			Description:           input.Experiment.Description,
			DatasetUUID:           input.Experiment.DatasetUUID,
			DatasetExternalID:     input.Experiment.DatasetExternalID,
			VersionUUID:           input.Experiment.VersionUUID,
			VersionExternalID:     input.Experiment.VersionExternalID,
			AgentExternalID:       input.Experiment.AgentExternalID,
			AgentVersion:          input.Experiment.AgentVersion,
			EnvironmentExternalID: input.Experiment.EnvironmentExternalID,
			TimeoutSeconds:        input.Experiment.TimeoutSeconds,
			EnvJSON:               encodeOptionalJSON(input.Experiment.Env),
			ResourcesJSON:         encodeOptionalJSON(input.Experiment.Resources),
			EvaluatorSetKey:       input.Experiment.EvaluatorSetKey,
			Status:                input.Experiment.Status,
			CreatedByUserUUID:     nullableString(input.Experiment.CreatedByUserUUID),
			CreatedAt:             input.Experiment.CreatedAt,
		})
		if insertErr != nil {
			return insertErr
		}
		bindings, bindErr := insertEvaluationExperimentBindings(ctx, executor, input.Bindings)
		if bindErr != nil {
			return bindErr
		}
		items, itemErr := insertEvaluationExperimentItems(ctx, executor, input.Items)
		if itemErr != nil {
			return itemErr
		}
		created = row.evaluationExperiment()
		created.Bindings = bindings
		created.Items = items
		created.ItemCount = int64(len(items))
		created.EvaluatorCount = int64(len(bindings))
		return nil
	})
	if isUniqueViolation(err) {
		return EvaluationExperiment{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetEvaluationExperiment(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationExperiment, error) {
	row, err := NewEvaluationExperimentMapper(d.mapperDB).FindByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationExperiment{}, mapNoRows(err)
	}
	experiment := row.evaluationExperiment()
	bindings, items, err := loadEvaluationExperimentChildren(ctx, d.mapperDB, organizationUUID, workspaceUUID, experiment.UUID)
	if err != nil {
		return EvaluationExperiment{}, err
	}
	experiment.Bindings = bindings
	experiment.Items = items
	return experiment, nil
}

func (d *DB) ListEvaluationExperimentsPage(ctx context.Context, params ListEvaluationExperimentsPageParams) ([]EvaluationExperiment, bool, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, err := NewEvaluationExperimentMapper(d.mapperDB).ListPage(ctx, evaluationExperimentPageParams{
		OrganizationUUID: params.OrganizationUUID,
		WorkspaceUUID:    params.WorkspaceUUID,
		FetchLimit:       limit + 1,
		Cursor:           params.Cursor,
	})
	if err != nil {
		return nil, false, err
	}
	experiments := make([]EvaluationExperiment, 0, len(rows))
	for _, row := range rows {
		experiments = append(experiments, row.evaluationExperiment())
	}
	hasMore := len(experiments) > limit
	if hasMore {
		experiments = experiments[:limit]
	}
	return experiments, hasMore, nil
}

func (d *DB) ListCompletedEvaluationExperiments(ctx context.Context, organizationUUID, workspaceUUID, versionExternalID, evaluatorSetKey string) ([]EvaluationExperiment, error) {
	rows, err := NewEvaluationExperimentMapper(d.mapperDB).ListMatching(ctx, evaluationExperimentCompareParams{
		OrganizationUUID:  organizationUUID,
		WorkspaceUUID:     workspaceUUID,
		VersionExternalID: versionExternalID,
		EvaluatorSetKey:   evaluatorSetKey,
		Status:            EvaluationExperimentStatusCompleted,
	})
	if err != nil {
		return nil, err
	}
	experiments := make([]EvaluationExperiment, 0, len(rows))
	for _, row := range rows {
		experiment := row.evaluationExperiment()
		bindings, listErr := NewEvaluationExperimentBindingMapper(d.mapperDB).ListByExperiment(ctx, organizationUUID, workspaceUUID, experiment.UUID)
		if listErr != nil {
			return nil, listErr
		}
		experiment.Bindings = evaluationExperimentBindings(bindings)
		experiments = append(experiments, experiment)
	}
	return experiments, nil
}

func (d *DB) UpdateEvaluationExperimentStatus(ctx context.Context, organizationUUID, workspaceUUID, externalID, status string, updatedAt time.Time) error {
	rows, err := NewEvaluationExperimentMapper(d.mapperDB).UpdateStatus(ctx, evaluationExperimentStatusParams{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		ExternalID:       externalID,
		Status:           status,
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

type EvaluationExperimentSessionMatch struct {
	Item                 EvaluationExperimentItem
	ExperimentExternalID string
}

func (d *DB) ListRunningEvaluationExperimentItemsBySession(ctx context.Context, organizationUUID, workspaceUUID, sessionExternalID string) ([]EvaluationExperimentSessionMatch, error) {
	rows, err := NewEvaluationExperimentItemMapper(d.mapperDB).ListRunningBySession(ctx, organizationUUID, workspaceUUID, sessionExternalID)
	if err != nil {
		return nil, err
	}
	matches := make([]EvaluationExperimentSessionMatch, 0, len(rows))
	for _, row := range rows {
		matches = append(matches, EvaluationExperimentSessionMatch{
			Item:                 row.evaluationExperimentItem(),
			ExperimentExternalID: row.ExperimentExternalID,
		})
	}
	return matches, nil
}

func (d *DB) FinishRunningEvaluationExperimentItem(ctx context.Context, organizationUUID, workspaceUUID, externalID, status, errorMessage string, updatedAt time.Time) (bool, error) {
	rows, err := NewEvaluationExperimentItemMapper(d.mapperDB).FinishRunning(ctx, evaluationExperimentItemFinishParams{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		ExternalID:       externalID,
		Status:           status,
		ErrorMessage:     errorMessage,
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (row evaluationExperimentSessionItemRow) evaluationExperimentItem() EvaluationExperimentItem {
	return evaluationExperimentItemRow{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		ExperimentUUID:    row.ExperimentUUID,
		CaseUUID:          row.CaseUUID,
		CaseExternalID:    row.CaseExternalID,
		Position:          row.Position,
		SessionExternalID: row.SessionExternalID,
		Status:            row.Status,
		ErrorMessage:      row.ErrorMessage,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}.evaluationExperimentItem()
}

func (d *DB) SetEvaluationExperimentItemSession(ctx context.Context, update EvaluationExperimentItemSession) error {
	rows, err := NewEvaluationExperimentItemMapper(d.mapperDB).SetSession(ctx, evaluationExperimentItemSessionParams{
		OrganizationUUID:  update.OrganizationUUID,
		WorkspaceUUID:     update.WorkspaceUUID,
		ExternalID:        update.ExternalID,
		SessionExternalID: nullableString(update.SessionExternalID),
		Status:            update.Status,
		ErrorMessage:      update.ErrorMessage,
		UpdatedAt:         update.UpdatedAt,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func insertEvaluationExperimentBindings(ctx context.Context, executor yourbatis.Executor, bindings []EvaluationExperimentBinding) ([]EvaluationExperimentBinding, error) {
	stored := make([]EvaluationExperimentBinding, 0, len(bindings))
	mapper := NewEvaluationExperimentBindingMapper(executor)
	for _, binding := range bindings {
		row, err := mapper.Insert(ctx, evaluationExperimentBindingWriteParams{
			UUID:                binding.UUID,
			ExternalID:          binding.ExternalID,
			OrganizationUUID:    binding.OrganizationUUID,
			WorkspaceUUID:       binding.WorkspaceUUID,
			ExperimentUUID:      binding.ExperimentUUID,
			Position:            binding.Position,
			EvaluatorUUID:       binding.EvaluatorUUID,
			EvaluatorExternalID: binding.EvaluatorExternalID,
			VersionUUID:         binding.VersionUUID,
			VersionExternalID:   binding.VersionExternalID,
			CreatedAt:           binding.CreatedAt,
		})
		if err != nil {
			return nil, err
		}
		stored = append(stored, row.evaluationExperimentBinding())
	}
	return stored, nil
}

func insertEvaluationExperimentItems(ctx context.Context, executor yourbatis.Executor, items []EvaluationExperimentItem) ([]EvaluationExperimentItem, error) {
	stored := make([]EvaluationExperimentItem, 0, len(items))
	mapper := NewEvaluationExperimentItemMapper(executor)
	for _, item := range items {
		row, err := mapper.Insert(ctx, evaluationExperimentItemWriteParams{
			UUID:             item.UUID,
			ExternalID:       item.ExternalID,
			OrganizationUUID: item.OrganizationUUID,
			WorkspaceUUID:    item.WorkspaceUUID,
			ExperimentUUID:   item.ExperimentUUID,
			CaseUUID:         item.CaseUUID,
			CaseExternalID:   item.CaseExternalID,
			Position:         item.Position,
			Status:           item.Status,
			CreatedAt:        item.CreatedAt,
		})
		if err != nil {
			return nil, err
		}
		stored = append(stored, row.evaluationExperimentItem())
	}
	return stored, nil
}

func loadEvaluationExperimentChildren(ctx context.Context, executor yourbatis.Executor, organizationUUID, workspaceUUID, experimentUUID string) ([]EvaluationExperimentBinding, []EvaluationExperimentItem, error) {
	bindings, err := NewEvaluationExperimentBindingMapper(executor).ListByExperiment(ctx, organizationUUID, workspaceUUID, experimentUUID)
	if err != nil {
		return nil, nil, err
	}
	items, err := NewEvaluationExperimentItemMapper(executor).ListByExperiment(ctx, organizationUUID, workspaceUUID, experimentUUID)
	if err != nil {
		return nil, nil, err
	}
	return evaluationExperimentBindings(bindings), evaluationExperimentItems(items), nil
}

func evaluationExperimentBindings(rows []evaluationExperimentBindingRow) []EvaluationExperimentBinding {
	bindings := make([]EvaluationExperimentBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, row.evaluationExperimentBinding())
	}
	return bindings
}

func evaluationExperimentItems(rows []evaluationExperimentItemRow) []EvaluationExperimentItem {
	items := make([]EvaluationExperimentItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.evaluationExperimentItem())
	}
	return items
}

func (row evaluationExperimentRow) evaluationExperiment() EvaluationExperiment {
	return EvaluationExperiment{
		UUID:                  row.UUID,
		ExternalID:            row.ExternalID,
		OrganizationUUID:      row.OrganizationUUID,
		WorkspaceUUID:         row.WorkspaceUUID,
		Name:                  row.Name,
		Description:           row.Description,
		DatasetUUID:           row.DatasetUUID,
		DatasetExternalID:     row.DatasetExternalID,
		VersionUUID:           row.VersionUUID,
		VersionExternalID:     row.VersionExternalID,
		AgentExternalID:       row.AgentExternalID,
		AgentVersion:          row.AgentVersion,
		EnvironmentExternalID: row.EnvironmentExternalID,
		TimeoutSeconds:        row.TimeoutSeconds,
		Env:                   decodeOptionalJSON(row.EnvJSON),
		Resources:             decodeOptionalJSON(row.ResourcesJSON),
		EvaluatorSetKey:       row.EvaluatorSetKey,
		Status:                row.Status,
		CreatedByUserUUID:     stringFromNullable(row.CreatedByUserUUID),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ItemCount:             row.ItemCount,
		SessionCount:          row.SessionCount,
		EvaluatorCount:        row.EvaluatorCount,
	}
}

func (row evaluationExperimentBindingRow) evaluationExperimentBinding() EvaluationExperimentBinding {
	return EvaluationExperimentBinding{
		UUID:                row.UUID,
		ExternalID:          row.ExternalID,
		OrganizationUUID:    row.OrganizationUUID,
		WorkspaceUUID:       row.WorkspaceUUID,
		ExperimentUUID:      row.ExperimentUUID,
		EvaluatorUUID:       row.EvaluatorUUID,
		EvaluatorExternalID: row.EvaluatorExternalID,
		VersionUUID:         row.VersionUUID,
		VersionExternalID:   row.VersionExternalID,
		Position:            row.Position,
		CreatedAt:           row.CreatedAt,
	}
}

func (row evaluationExperimentItemRow) evaluationExperimentItem() EvaluationExperimentItem {
	return EvaluationExperimentItem{
		UUID:              row.UUID,
		ExternalID:        row.ExternalID,
		OrganizationUUID:  row.OrganizationUUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		ExperimentUUID:    row.ExperimentUUID,
		CaseUUID:          row.CaseUUID,
		CaseExternalID:    row.CaseExternalID,
		Position:          row.Position,
		SessionExternalID: stringFromNullable(row.SessionExternalID),
		Status:            row.Status,
		ErrorMessage:      row.ErrorMessage,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
