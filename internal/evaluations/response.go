package evaluations

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/google/uuid"
)

type datasetVersionResponse struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	CaseCount int    `json:"case_count"`
}

type datasetResponse struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Name      string                 `json:"name"`
	Desc      string                 `json:"desc"`
	Status    string                 `json:"status"`
	Version   datasetVersionResponse `json:"version"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
}

type datasetPageResponse struct {
	Data     []datasetResponse `json:"data"`
	NextPage *string           `json:"next_page"`
}

type caseResponse struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	DatasetID      string          `json:"dataset_id"`
	VersionID      string          `json:"version_id"`
	Position       int             `json:"position"`
	Name           string          `json:"name"`
	Desc           string          `json:"desc"`
	Turns          []string        `json:"turns"`
	Checkpoints    json.RawMessage `json:"checkpoints"`
	InputFiles     json.RawMessage `json:"input_files"`
	ReferenceFiles json.RawMessage `json:"reference_files"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

type casePageResponse struct {
	Data     []caseResponse `json:"data"`
	NextPage *string        `json:"next_page"`
}

type datasetCursorWire struct {
	CreatedAt time.Time `json:"created_at"`
	UUID      string    `json:"uuid"`
}

type caseCursorWire struct {
	Position int    `json:"position"`
	UUID     string `json:"uuid"`
}

func datasetResponseFrom(dataset db.EvaluationDataset) datasetResponse {
	status := "available"
	if dataset.ArchivedAt != nil {
		status = "archived"
	}
	return datasetResponse{
		ID:     dataset.ExternalID,
		Type:   "evaluation_dataset",
		Name:   dataset.Name,
		Desc:   dataset.Description,
		Status: status,
		Version: datasetVersionResponse{
			ID:        dataset.Version.ExternalID,
			Version:   dataset.Version.Version,
			CaseCount: dataset.Version.CaseCount,
		},
		CreatedAt: dataset.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: dataset.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func datasetPage(datasets []db.EvaluationDataset, hasMore bool) datasetPageResponse {
	data := make([]datasetResponse, 0, len(datasets))
	for _, dataset := range datasets {
		data = append(data, datasetResponseFrom(dataset))
	}
	var nextPage *string
	if hasMore && len(datasets) > 0 {
		value := encodeCursor(datasetCursorWire{CreatedAt: datasets[len(datasets)-1].CreatedAt, UUID: datasets[len(datasets)-1].UUID})
		nextPage = &value
	}
	return datasetPageResponse{Data: data, NextPage: nextPage}
}

func caseResponseFrom(item db.EvaluationCase) caseResponse {
	turns := item.Turns
	if turns == nil {
		turns = []string{}
	}
	return caseResponse{
		ID:             item.ExternalID,
		Type:           "evaluation_case",
		DatasetID:      item.DatasetExternalID,
		VersionID:      item.VersionExternalID,
		Position:       item.Position,
		Name:           item.Name,
		Desc:           item.Description,
		Turns:          turns,
		Checkpoints:    item.Checkpoints,
		InputFiles:     item.InputFiles,
		ReferenceFiles: item.ReferenceFiles,
		CreatedAt:      item.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func casePage(cases []db.EvaluationCase, hasMore bool) casePageResponse {
	data := make([]caseResponse, 0, len(cases))
	for _, item := range cases {
		data = append(data, caseResponseFrom(item))
	}
	var nextPage *string
	if hasMore && len(cases) > 0 {
		last := cases[len(cases)-1]
		value := encodeCursor(caseCursorWire{Position: last.Position, UUID: last.UUID})
		nextPage = &value
	}
	return casePageResponse{Data: data, NextPage: nextPage}
}

func pageQuery[T any](r *http.Request, decode func(string) (*T, error)) (int, *T, error) {
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return 0, nil, invalidLimit(err)
	}
	cursor, err := decode(r.URL.Query().Get("page"))
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func decodeDatasetCursor(raw string) (*db.EvaluationDatasetPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var wire datasetCursorWire
	if err := decodeCursor(raw, &wire); err != nil {
		return nil, err
	}
	if wire.CreatedAt.IsZero() {
		return nil, invalidPage(errInvalidPageCursor)
	}
	parsed, err := uuid.Parse(strings.TrimSpace(wire.UUID))
	if err != nil {
		return nil, invalidPage(err)
	}
	return &db.EvaluationDatasetPageCursor{CreatedAt: wire.CreatedAt, UUID: parsed.String()}, nil
}

func decodeCaseCursor(raw string) (*db.EvaluationCasePageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var wire caseCursorWire
	if err := decodeCursor(raw, &wire); err != nil {
		return nil, err
	}
	if wire.Position < 1 {
		return nil, invalidPage(errInvalidPageCursor)
	}
	parsed, err := uuid.Parse(strings.TrimSpace(wire.UUID))
	if err != nil {
		return nil, invalidPage(err)
	}
	return &db.EvaluationCasePageCursor{Position: wire.Position, UUID: parsed.String()}, nil
}

func encodeCursor(value any) string {
	payload, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(raw string, dest any) error {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return invalidPage(err)
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return invalidPage(err)
	}
	return nil
}
