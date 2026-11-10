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
		CreatedAt: formatTime(item.CreatedAt), UpdatedAt: formatTime(item.UpdatedAt),
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
