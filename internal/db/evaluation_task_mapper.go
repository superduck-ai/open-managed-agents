package db

import (
	"context"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationTaskMapper -sql ./evaluation_task_mapper.xml -out ./evaluation_task_mapper.sqlmap.gen.go -dialect postgres

type evaluationTaskRow struct {
	UUID                     string          `db:"uuid"`
	ExternalID               string          `db:"external_id"`
	OrganizationUUID         string          `db:"organization_uuid"`
	WorkspaceUUID            string          `db:"workspace_uuid"`
	CreatedByUserUUID        *string         `db:"created_by_user_uuid"`
	CreatedByAPIKeyUUID      *string         `db:"created_by_api_key_uuid"`
	Name                     string          `db:"name"`
	Mode                     string          `db:"mode"`
	Status                   string          `db:"status"`
	AgentExternalID          string          `db:"agent_external_id"`
	AgentVersion             int             `db:"agent_version"`
	EnvironmentExternalID    string          `db:"environment_external_id"`
	DatasetExternalID        string          `db:"dataset_external_id"`
	DatasetVersionExternalID string          `db:"dataset_version_external_id"`
	RubricExternalID         string          `db:"rubric_external_id"`
	RubricVersionExternalID  string          `db:"rubric_version_external_id"`
	PassScore                int             `db:"pass_score"`
	RubricBody               string          `db:"rubric_body"`
	RubricModel              string          `db:"rubric_model"`
	Strategy                 string          `db:"strategy"`
	LowScoreThreshold        int             `db:"low_score_threshold"`
	OptimizeSystem           bool            `db:"optimize_system"`
	BaselineSystem           string          `db:"baseline_system"`
	CandidateSystem          string          `db:"candidate_system"`
	CandidateSummary         string          `db:"candidate_summary"`
	ErrorMessage             string          `db:"error_message"`
	VaultIDs                 json.RawMessage `db:"vault_ids"`
	Resources                json.RawMessage `db:"resources"`
	AppliedAt                *time.Time      `db:"applied_at"`
	CreatedAt                time.Time       `db:"created_at"`
	UpdatedAt                time.Time       `db:"updated_at"`
}

type evaluationTaskItemRow struct {
	UUID              string    `db:"uuid"`
	ExternalID        string    `db:"external_id"`
	OrganizationUUID  string    `db:"organization_uuid"`
	WorkspaceUUID     string    `db:"workspace_uuid"`
	TaskUUID          string    `db:"task_uuid"`
	TaskExternalID    string    `db:"task_external_id"`
	CaseExternalID    string    `db:"case_external_id"`
	Round             int       `db:"round"`
	Position          int       `db:"position"`
	SessionExternalID string    `db:"session_external_id"`
	Status            string    `db:"status"`
	ErrorMessage      string    `db:"error_message"`
	Query             string    `db:"query"`
	ExpectedAnswer    string    `db:"expected_answer"`
	ExpectedToolTrace []byte    `db:"expected_tool_trace"`
	Acceptance        string    `db:"acceptance"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

type evaluationTaskScoreRow struct {
	UUID             string          `db:"uuid"`
	ExternalID       string          `db:"external_id"`
	OrganizationUUID string          `db:"organization_uuid"`
	WorkspaceUUID    string          `db:"workspace_uuid"`
	TaskUUID         string          `db:"task_uuid"`
	ItemUUID         string          `db:"item_uuid"`
	Status           string          `db:"status"`
	Score            float64         `db:"score"`
	Passed           bool            `db:"passed"`
	Explanation      string          `db:"explanation"`
	Answer           string          `db:"answer"`
	Dimensions       json.RawMessage `db:"dimensions"`
	ErrorMessage     string          `db:"error_message"`
	CreatedAt        time.Time       `db:"created_at"`
}

type evaluationTaskUsageRow struct {
	InputTokens          int64 `db:"input_tokens"`
	OutputTokens         int64 `db:"output_tokens"`
	CacheReadInputTokens int64 `db:"cache_read_input_tokens"`
}

type insertEvaluationTaskParams struct {
	UUID                     string
	ExternalID               string
	OrganizationUUID         string
	WorkspaceUUID            string
	CreatedByUserUUID        *string
	CreatedByAPIKeyUUID      *string
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
	VaultIDs                 json.RawMessage
	Resources                json.RawMessage
	CreatedAt                time.Time
}

type insertEvaluationTaskItemParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	TaskUUID          string
	TaskExternalID    string
	CaseExternalID    string
	Round             int
	Position          int
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	Acceptance        string
	CreatedAt         time.Time
}

type insertEvaluationTaskScoreParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	TaskUUID         string
	ItemUUID         string
	Status           string
	Score            float64
	Passed           bool
	Explanation      string
	Answer           string
	Dimensions       json.RawMessage
	ErrorMessage     string
	CreatedAt        time.Time
}

type evaluationItemStatusParams struct {
	UUID              string
	OrganizationUUID  string
	WorkspaceUUID     string
	Status            string
	ErrorMessage      string
	SessionExternalID string
	UpdatedAt         time.Time
}

type evaluationTaskStatusParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExternalID       string
	Status           string
	ErrorMessage     string
	UpdatedAt        time.Time
}

type evaluationTaskIDParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	TaskUUIDs        []string
}

type evaluationResetItemsParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	TaskUUID         string
	Round            int
	UpdatedAt        time.Time
}

type EvaluationTaskMapper interface {
	InsertTask(ctx context.Context, params insertEvaluationTaskParams) (evaluationTaskRow, error)
	InsertItem(ctx context.Context, params insertEvaluationTaskItemParams) (evaluationTaskItemRow, error)
	FindTask(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationTaskRow, error)
	ListTasks(ctx context.Context, params evaluationListParams) ([]evaluationTaskRow, error)
	ListItems(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string) ([]evaluationTaskItemRow, error)
	ListScores(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string) ([]evaluationTaskScoreRow, error)
	ListItemsByTasks(ctx context.Context, params evaluationTaskIDParams) ([]evaluationTaskItemRow, error)
	ListScoresByTasks(ctx context.Context, params evaluationTaskIDParams) ([]evaluationTaskScoreRow, error)
	ListRunningBySession(ctx context.Context, organizationUUID, workspaceUUID, sessionExternalID string) ([]evaluationTaskItemRow, error)
	MarkItemRunning(ctx context.Context, params evaluationItemStatusParams) (int64, error)
	FinishItem(ctx context.Context, params evaluationItemStatusParams) (int64, error)
	ResetFailedItems(ctx context.Context, params evaluationResetItemsParams) (int64, error)
	CancelPendingItems(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string, updatedAt time.Time) (int64, error)
	UpdateTaskStatus(ctx context.Context, params evaluationTaskStatusParams) (int64, error)
	InsertScore(ctx context.Context, params insertEvaluationTaskScoreParams) (evaluationTaskScoreRow, error)
	SumTaskUsage(ctx context.Context, organizationUUID, workspaceUUID, taskUUID string) (evaluationTaskUsageRow, error)
}
