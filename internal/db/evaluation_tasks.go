package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type EvaluationTask struct {
	UUID                     string
	ExternalID               string
	OrganizationUUID         string
	WorkspaceUUID            string
	CreatedByUserUUID        string
	CreatedByAPIKeyUUID      string
	Name                     string
	Mode                     string
	Status                   string
	AgentExternalID          string
	AgentVersion             int
	EnvironmentExternalID    string
	DatasetExternalID        string
	DatasetVersionExternalID string
	RubricExternalID         string
	RubricVersionExternalID  string
	PassScore                int
	RubricBody               string
	RubricModel              string
	Strategy                 string
	LowScoreThreshold        int
	OptimizeSystem           bool
	BaselineSystem           string
	CandidateSystem          string
	CandidateSummary         string
	ErrorMessage             string
	VaultIDs                 json.RawMessage
	Resources                json.RawMessage
	AppliedAt                *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type EvaluationTaskItem struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	TaskUUID          string
	TaskExternalID    string
	CaseExternalID    string
	Round             int
	Position          int
	SessionExternalID string
	Status            string
	ErrorMessage      string
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	Acceptance        string
	Inputs            json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EvaluationTaskScore struct {
	UUID               string
	ExternalID         string
	OrganizationUUID   string
	WorkspaceUUID      string
	TaskUUID           string
	ItemUUID           string
	Status             string
	Score              float64
	Passed             bool
	Explanation        string
	Answer             string
	Dimensions         json.RawMessage
	ErrorMessage       string
	JudgeModel         string
	JudgePromptVersion string
	JudgeSystem        string
	JudgePrompt        string
	JudgeResponse      string
	CreatedAt          time.Time
}

type EvaluationTaskRef struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExternalID       string
}

type EvaluationTaskRetry struct {
	ResetItems    int64
	ClearedScores int64
}

type EvaluationTaskUsage struct {
	InputTokens          int64
	OutputTokens         int64
	CacheReadInputTokens int64
}

type EvaluationTaskDetail struct {
	Task   EvaluationTask
	Items  []EvaluationTaskItem
	Scores []EvaluationTaskScore
}

func (d *DB) CreateEvaluationTask(ctx context.Context, task EvaluationTask, items []EvaluationTaskItem) (EvaluationTaskDetail, error) {
	var detail EvaluationTaskDetail
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEvaluationTaskMapper(executor)
		row, err := mapper.InsertTask(ctx, insertEvaluationTaskParams{
			UUID: task.UUID, ExternalID: task.ExternalID, OrganizationUUID: task.OrganizationUUID, WorkspaceUUID: task.WorkspaceUUID,
			CreatedByUserUUID: uuidPointer(task.CreatedByUserUUID), CreatedByAPIKeyUUID: uuidPointer(task.CreatedByAPIKeyUUID), Name: task.Name, Mode: task.Mode,
			Status: task.Status, AgentExternalID: task.AgentExternalID, AgentVersion: task.AgentVersion, EnvironmentExternalID: task.EnvironmentExternalID,
			DatasetExternalID: task.DatasetExternalID, DatasetVersionExternalID: task.DatasetVersionExternalID, RubricExternalID: task.RubricExternalID,
			RubricVersionExternalID: task.RubricVersionExternalID, PassScore: task.PassScore, RubricBody: task.RubricBody, RubricModel: task.RubricModel,
			Strategy: task.Strategy, LowScoreThreshold: task.LowScoreThreshold, OptimizeSystem: task.OptimizeSystem, BaselineSystem: task.BaselineSystem,
			VaultIDs: task.VaultIDs, Resources: task.Resources, CreatedAt: task.CreatedAt,
		})
		if err != nil {
			return mapEvaluationWrite(err)
		}
		detail.Task = taskFromRow(row)
		detail.Items = make([]EvaluationTaskItem, 0, len(items))
		for _, item := range items {
			inserted, err := mapper.InsertItem(ctx, insertEvaluationTaskItemParams{
				UUID: item.UUID, ExternalID: item.ExternalID, OrganizationUUID: task.OrganizationUUID, WorkspaceUUID: task.WorkspaceUUID,
				TaskUUID: row.UUID, TaskExternalID: row.ExternalID, CaseExternalID: item.CaseExternalID, Round: item.Round, Position: item.Position,
				Query: item.Query, ExpectedAnswer: item.ExpectedAnswer, ExpectedToolTrace: item.ExpectedToolTrace, Acceptance: item.Acceptance, Inputs: item.Inputs, CreatedAt: task.CreatedAt,
			})
			if err != nil {
				return mapEvaluationWrite(err)
			}
			detail.Items = append(detail.Items, itemFromRow(inserted))
		}
		return nil
	})
	return detail, err
}

func (d *DB) ListEvaluationTaskChildren(ctx context.Context, organizationUUID, workspaceUUID string, taskUUIDs []string) ([]EvaluationTaskItem, []EvaluationTaskScore, error) {
	if len(taskUUIDs) == 0 {
		return nil, nil, nil
	}
	mapper := NewEvaluationTaskMapper(d.mapperDB)
	params := evaluationTaskIDParams{OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, TaskUUIDs: taskUUIDs}
	itemRows, err := mapper.ListItemsByTasks(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	scoreRows, err := mapper.ListScoresByTasks(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	items := make([]EvaluationTaskItem, 0, len(itemRows))
	for _, row := range itemRows {
		items = append(items, itemFromRow(row))
	}
	scores := make([]EvaluationTaskScore, 0, len(scoreRows))
	for _, row := range scoreRows {
		scores = append(scores, scoreFromRow(row))
	}
	return items, scores, nil
}

func (d *DB) ListEvaluationTasks(ctx context.Context, organizationUUID, workspaceUUID string, offset, limit int) ([]EvaluationTask, error) {
	rows, err := NewEvaluationTaskMapper(d.mapperDB).ListTasks(ctx, evaluationListParams{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	tasks := make([]EvaluationTask, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, taskFromRow(row))
	}
	return tasks, nil
}

func (d *DB) GetEvaluationTask(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (EvaluationTaskDetail, error) {
	mapper := NewEvaluationTaskMapper(d.mapperDB)
	row, err := mapper.FindTask(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return EvaluationTaskDetail{}, mapNoRows(err)
	}
	items, err := mapper.ListItems(ctx, organizationUUID, workspaceUUID, row.UUID)
	if err != nil {
		return EvaluationTaskDetail{}, err
	}
	scores, err := mapper.ListScores(ctx, organizationUUID, workspaceUUID, row.UUID)
	if err != nil {
		return EvaluationTaskDetail{}, err
	}
	detail := EvaluationTaskDetail{Task: taskFromRow(row), Items: make([]EvaluationTaskItem, 0, len(items)), Scores: make([]EvaluationTaskScore, 0, len(scores))}
	for _, item := range items {
		detail.Items = append(detail.Items, itemFromRow(item))
	}
	for _, score := range scores {
		detail.Scores = append(detail.Scores, scoreFromRow(score))
	}
	return detail, nil
}

func (d *DB) ListRunningEvaluationItemsBySession(ctx context.Context, organizationUUID, workspaceUUID, sessionExternalID string) ([]EvaluationTaskItem, error) {
	rows, err := NewEvaluationTaskMapper(d.mapperDB).ListRunningBySession(ctx, organizationUUID, workspaceUUID, sessionExternalID)
	if err != nil {
		return nil, err
	}
	items := make([]EvaluationTaskItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, itemFromRow(row))
	}
	return items, nil
}

func (d *DB) MarkEvaluationItemRunning(ctx context.Context, organizationUUID, workspaceUUID, itemUUID, sessionExternalID string, updatedAt time.Time) (bool, error) {
	rows, err := NewEvaluationTaskMapper(d.mapperDB).MarkItemRunning(ctx, evaluationItemStatusParams{
		UUID: itemUUID, OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, SessionExternalID: sessionExternalID, UpdatedAt: updatedAt,
	})
	return rows > 0, err
}

func (d *DB) FinishRunningEvaluationItem(ctx context.Context, organizationUUID, workspaceUUID, itemUUID, status, errorMessage string, updatedAt time.Time) (bool, error) {
	rows, err := NewEvaluationTaskMapper(d.mapperDB).FinishItem(ctx, evaluationItemStatusParams{
		UUID: itemUUID, OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Status: status, ErrorMessage: errorMessage, UpdatedAt: updatedAt,
	})
	return rows > 0, err
}

func (d *DB) TouchRunningEvaluationItem(ctx context.Context, organizationUUID, workspaceUUID, itemUUID string, updatedAt time.Time) error {
	_, err := NewEvaluationTaskMapper(d.mapperDB).TouchRunningItem(ctx, evaluationItemStatusParams{
		UUID: itemUUID, OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, UpdatedAt: updatedAt,
	})
	return err
}

func (d *DB) RetryEvaluationTask(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string, round int, updatedAt time.Time) (EvaluationTaskRetry, error) {
	var result EvaluationTaskRetry
	params := evaluationRoundParams{OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, TaskUUID: taskUUID, Round: round, UpdatedAt: updatedAt}
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEvaluationTaskMapper(executor)
		restarted, err := mapper.RestartTask(ctx, params)
		if err != nil {
			return err
		}
		if restarted == 0 {
			return ErrInvalidState
		}
		if result.ResetItems, err = mapper.ResetFailedItems(ctx, params); err != nil {
			return err
		}
		if result.ClearedScores, err = mapper.DeleteFailedScores(ctx, params); err != nil {
			return err
		}
		if result.ResetItems == 0 && result.ClearedScores == 0 {
			return ErrPreconditionFailed
		}
		return nil
	})
	return result, err
}

func (d *DB) ListSettleableEvaluationTasks(ctx context.Context, updatedBefore time.Time, limit int) ([]EvaluationTaskRef, error) {
	rows, err := NewEvaluationTaskMapper(d.mapperDB).ListSettleableTasks(ctx, updatedBefore, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]EvaluationTaskRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, EvaluationTaskRef(row))
	}
	return refs, nil
}

func (d *DB) ListStaleRunningEvaluationItems(ctx context.Context, updatedBefore time.Time, limit int) ([]EvaluationTaskItem, error) {
	rows, err := NewEvaluationTaskMapper(d.mapperDB).ListStaleRunningItems(ctx, updatedBefore, limit)
	if err != nil {
		return nil, err
	}
	items := make([]EvaluationTaskItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, itemFromRow(row))
	}
	return items, nil
}

func (d *DB) CancelPendingEvaluationItems(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string, updatedAt time.Time) error {
	_, err := NewEvaluationTaskMapper(d.mapperDB).CancelPendingItems(ctx, organizationUUID, workspaceUUID, taskUUID, updatedAt)
	return err
}

func (d *DB) UpdateEvaluationTaskStatus(ctx context.Context, organizationUUID, workspaceUUID, externalID, status, errorMessage string, updatedAt time.Time) error {
	_, err := NewEvaluationTaskMapper(d.mapperDB).UpdateTaskStatus(ctx, evaluationTaskStatusParams{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExternalID: externalID, Status: status, ErrorMessage: errorMessage, UpdatedAt: updatedAt,
	})
	return err
}

func (d *DB) CreateEvaluationTaskScore(ctx context.Context, score EvaluationTaskScore) (EvaluationTaskScore, error) {
	row, err := NewEvaluationTaskMapper(d.mapperDB).InsertScore(ctx, insertEvaluationTaskScoreParams{
		UUID: score.UUID, ExternalID: score.ExternalID, OrganizationUUID: score.OrganizationUUID, WorkspaceUUID: score.WorkspaceUUID,
		TaskUUID: score.TaskUUID, ItemUUID: score.ItemUUID, Status: score.Status, Score: score.Score, Passed: score.Passed,
		Explanation: score.Explanation, Answer: score.Answer, Dimensions: score.Dimensions, ErrorMessage: score.ErrorMessage,
		JudgeModel: score.JudgeModel, JudgePromptVersion: score.JudgePromptVersion,
		JudgeSystem: score.JudgeSystem, JudgePrompt: score.JudgePrompt, JudgeResponse: score.JudgeResponse, CreatedAt: score.CreatedAt,
	})
	if err != nil {
		return EvaluationTaskScore{}, mapEvaluationWrite(err)
	}
	return scoreFromRow(row), nil
}

func (d *DB) SumEvaluationTaskUsage(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string) (EvaluationTaskUsage, error) {
	row, err := NewEvaluationTaskMapper(d.mapperDB).SumTaskUsage(ctx, organizationUUID, workspaceUUID, taskUUID)
	if err != nil {
		return EvaluationTaskUsage{}, err
	}
	return EvaluationTaskUsage{
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, CacheReadInputTokens: row.CacheReadInputTokens,
	}, nil
}

func uuidPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func taskFromRow(row evaluationTaskRow) EvaluationTask {
	return EvaluationTask{
		UUID: row.UUID, ExternalID: row.ExternalID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID,
		CreatedByUserUUID: stringOrEmpty(row.CreatedByUserUUID), CreatedByAPIKeyUUID: stringOrEmpty(row.CreatedByAPIKeyUUID), Name: row.Name, Mode: row.Mode, Status: row.Status,
		AgentExternalID: row.AgentExternalID, AgentVersion: row.AgentVersion, EnvironmentExternalID: row.EnvironmentExternalID,
		DatasetExternalID: row.DatasetExternalID, DatasetVersionExternalID: row.DatasetVersionExternalID, RubricExternalID: row.RubricExternalID,
		RubricVersionExternalID: row.RubricVersionExternalID, PassScore: row.PassScore, RubricBody: row.RubricBody, RubricModel: row.RubricModel,
		Strategy: row.Strategy, LowScoreThreshold: row.LowScoreThreshold, OptimizeSystem: row.OptimizeSystem, BaselineSystem: row.BaselineSystem,
		CandidateSystem: row.CandidateSystem, CandidateSummary: row.CandidateSummary, ErrorMessage: row.ErrorMessage, VaultIDs: row.VaultIDs,
		Resources: row.Resources, AppliedAt: row.AppliedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func itemFromRow(row evaluationTaskItemRow) EvaluationTaskItem {
	return EvaluationTaskItem{
		UUID: row.UUID, ExternalID: row.ExternalID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID,
		TaskUUID: row.TaskUUID, TaskExternalID: row.TaskExternalID, CaseExternalID: row.CaseExternalID, Round: row.Round, Position: row.Position,
		SessionExternalID: row.SessionExternalID, Status: row.Status, ErrorMessage: row.ErrorMessage, Query: row.Query, ExpectedAnswer: row.ExpectedAnswer,
		ExpectedToolTrace: rawJSON(row.ExpectedToolTrace), Acceptance: row.Acceptance, Inputs: rawJSON(row.Inputs), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func scoreFromRow(row evaluationTaskScoreRow) EvaluationTaskScore {
	return EvaluationTaskScore{
		UUID: row.UUID, ExternalID: row.ExternalID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID,
		TaskUUID: row.TaskUUID, ItemUUID: row.ItemUUID, Status: row.Status, Score: row.Score, Passed: row.Passed, Explanation: row.Explanation,
		Answer: row.Answer, Dimensions: row.Dimensions, ErrorMessage: row.ErrorMessage, JudgeModel: row.JudgeModel,
		JudgePromptVersion: row.JudgePromptVersion, JudgeSystem: row.JudgeSystem, JudgePrompt: row.JudgePrompt,
		JudgeResponse: row.JudgeResponse, CreatedAt: row.CreatedAt,
	}
}
