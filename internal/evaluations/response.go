package evaluations

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type pageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

func writePage[T any](w http.ResponseWriter, data []T, next *string) {
	if data == nil {
		data = []T{}
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[T]{Data: data, NextPage: next})
}

func encodeOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, invalidArgument(pageInvalidMessage, err)
	}
	offset, err := strconv.Atoi(string(payload))
	if err != nil || offset < 0 {
		return 0, invalidArgument(pageInvalidMessage, err)
	}
	return offset, nil
}

func nextPage(offset, limit, count int) *string {
	if count <= limit {
		return nil
	}
	encoded := encodeOffset(offset + limit)
	return &encoded
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

type datasetResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Desc      string `json:"desc"`
	VersionID string `json:"version_id"`
	Version   int    `json:"version"`
	CaseCount int    `json:"case_count"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func datasetJSON(dataset db.EvaluationDataset) datasetResponse {
	return datasetResponse{
		ID: dataset.ExternalID, Type: "evaluation_dataset", Name: dataset.Name, Desc: dataset.Description,
		VersionID: dataset.VersionExternalID, Version: dataset.Version, CaseCount: dataset.CaseCount,
		CreatedAt: formatTime(dataset.CreatedAt), UpdatedAt: formatTime(dataset.UpdatedAt),
	}
}

type caseResponse struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Query             string          `json:"query"`
	ExpectedAnswer    string          `json:"expected_answer"`
	ExpectedToolTrace json.RawMessage `json:"expected_tool_trace"`
	SourceSessionID   string          `json:"source_session_id"`
	Polarity          string          `json:"polarity"`
	Acceptance        string          `json:"acceptance"`
	Inputs            json.RawMessage `json:"inputs"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

func caseJSON(item db.EvaluationCase) caseResponse {
	trace := item.ExpectedToolTrace
	if len(trace) == 0 {
		trace = json.RawMessage(`null`)
	}
	return caseResponse{
		ID: item.ExternalID, Type: "evaluation_case", Query: item.Query, ExpectedAnswer: item.ExpectedAnswer,
		ExpectedToolTrace: trace, SourceSessionID: item.SourceSessionID, Polarity: item.Polarity, Acceptance: item.Acceptance,
		Inputs: item.Inputs, CreatedAt: formatTime(item.CreatedAt), UpdatedAt: formatTime(item.UpdatedAt),
	}
}

type rubricResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Desc      string `json:"desc"`
	VersionID string `json:"version_id"`
	Version   int    `json:"version"`
	Body      string `json:"body"`
	PassScore int    `json:"pass_score"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type rubricVersionResponse struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	Body      string `json:"body"`
	PassScore int    `json:"pass_score"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
}

func rubricVersionJSON(rubric db.EvaluationRubric) rubricVersionResponse {
	return rubricVersionResponse{
		ID: rubric.VersionExternalID, Version: rubric.Version, Body: rubric.Body, PassScore: rubric.PassScore,
		Model: rubric.Model, CreatedAt: formatTime(rubric.CreatedAt),
	}
}

func rubricJSON(rubric db.EvaluationRubric) rubricResponse {
	return rubricResponse{
		ID: rubric.ExternalID, Type: "evaluation_rubric", Name: rubric.Name, Desc: rubric.Description,
		VersionID: rubric.VersionExternalID, Version: rubric.Version, Body: rubric.Body, PassScore: rubric.PassScore,
		Model: rubric.Model, CreatedAt: formatTime(rubric.CreatedAt), UpdatedAt: formatTime(rubric.UpdatedAt),
	}
}

type summaryResponse struct {
	Round      int      `json:"round"`
	Score      *float64 `json:"score"`
	PassRate   *float64 `json:"pass_rate"`
	Scored     int      `json:"scored"`
	Passed     int      `json:"passed"`
	Exceptions int      `json:"exceptions"`
	Total      int      `json:"total"`
}

func summaryJSON(summary RoundSummary) summaryResponse {
	return summaryResponse{
		Round: summary.Round, Score: summary.Score, PassRate: summary.PassRate, Scored: summary.Scored,
		Passed: summary.Passed, Exceptions: summary.Exceptions, Total: summary.Total,
	}
}

type scoreResponse struct {
	Status       string          `json:"status"`
	Score        float64         `json:"score"`
	Passed       bool            `json:"passed"`
	Explanation  string          `json:"explanation"`
	Answer       string          `json:"answer"`
	Dimensions   json.RawMessage `json:"dimensions"`
	ErrorMessage string          `json:"error_message"`
}

type itemResponse struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Round          int            `json:"round"`
	CaseID         string         `json:"case_id"`
	Query          string         `json:"query"`
	ExpectedAnswer string         `json:"expected_answer"`
	Acceptance     string         `json:"acceptance"`
	SessionID      string         `json:"session_id"`
	Status         string         `json:"status"`
	ErrorMessage   string         `json:"error_message"`
	Score          *scoreResponse `json:"score"`
}

type taskResponse struct {
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Name             string            `json:"name"`
	Mode             string            `json:"mode"`
	Status           string            `json:"status"`
	AgentID          string            `json:"agent_id"`
	AgentVersion     int               `json:"agent_version"`
	EnvironmentID    string            `json:"environment_id"`
	DatasetID        string            `json:"dataset_id"`
	DatasetVersionID string            `json:"dataset_version_id"`
	RubricID         string            `json:"rubric_id"`
	RubricVersionID  string            `json:"rubric_version_id"`
	PassScore        int               `json:"pass_score"`
	ErrorMessage     string            `json:"error_message"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	ResourceCount    int               `json:"resource_count"`
	VaultCount       int               `json:"vault_count"`
	AgentName        string            `json:"agent_name,omitempty"`
	DatasetName      string            `json:"dataset_name,omitempty"`
	RubricName       string            `json:"rubric_name,omitempty"`
	Usage            *usageResponse    `json:"usage,omitempty"`
	Summary          summaryResponse   `json:"summary"`
	Rounds           []summaryResponse `json:"rounds,omitempty"`
	Items            []itemResponse    `json:"items,omitempty"`
}

func arrayLength(raw json.RawMessage) int {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return 0
	}
	return len(values)
}

func taskJSON(detail db.EvaluationTaskDetail, includeItems bool) taskResponse {
	task := detail.Task
	response := taskResponse{
		ID: task.ExternalID, Type: "evaluation_task", Name: task.Name, Mode: task.Mode, Status: task.Status,
		AgentID: task.AgentExternalID, AgentVersion: task.AgentVersion, EnvironmentID: task.EnvironmentExternalID,
		DatasetID: task.DatasetExternalID, DatasetVersionID: task.DatasetVersionExternalID, RubricID: task.RubricExternalID,
		RubricVersionID: task.RubricVersionExternalID, PassScore: task.PassScore, ErrorMessage: task.ErrorMessage,
		CreatedAt: formatTime(task.CreatedAt), UpdatedAt: formatTime(task.UpdatedAt),
		ResourceCount: arrayLength(task.Resources), VaultCount: arrayLength(task.VaultIDs),
		Summary: summaryJSON(SummarizeRound(detail.Items, detail.Scores, latestRound(detail.Items))),
	}
	seen := map[int]struct{}{}
	for _, item := range detail.Items {
		if _, ok := seen[item.Round]; ok {
			continue
		}
		seen[item.Round] = struct{}{}
		response.Rounds = append(response.Rounds, summaryJSON(SummarizeRound(detail.Items, detail.Scores, item.Round)))
	}
	if !includeItems {
		return response
	}
	scores := map[string]db.EvaluationTaskScore{}
	for _, score := range detail.Scores {
		scores[score.ItemUUID] = score
	}
	for _, item := range detail.Items {
		row := itemResponse{
			ID: item.ExternalID, Type: "evaluation_task_item", Round: item.Round, CaseID: item.CaseExternalID,
			Query: item.Query, ExpectedAnswer: item.ExpectedAnswer, Acceptance: item.Acceptance, SessionID: item.SessionExternalID,
			Status: item.Status, ErrorMessage: item.ErrorMessage,
		}
		if score, ok := scores[item.UUID]; ok {
			dimensions := score.Dimensions
			if len(dimensions) == 0 {
				dimensions = json.RawMessage(`[]`)
			}
			row.Score = &scoreResponse{
				Status: score.Status, Score: score.Score, Passed: score.Passed, Explanation: score.Explanation,
				Answer: score.Answer, Dimensions: dimensions, ErrorMessage: score.ErrorMessage,
			}
		}
		response.Items = append(response.Items, row)
	}
	return response
}
