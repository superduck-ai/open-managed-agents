package evaluations

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestEvaluationRoutesRejectInvalidInputBeforeStore(t *testing.T) {
	store := &evaluationStoreStub{}
	router := evaluationRouter(store)
	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		status      int
		want        string
	}{
		{name: "empty name", method: http.MethodPost, path: evaluationDatasetPath(""), body: `{"name":"  "}`, status: http.StatusBadRequest, want: nameRequiredMessage},
		{name: "empty case name", method: http.MethodPost, path: evaluationDatasetPath("/evalds_test/cases"), body: `{"name":" ","turns":["hello"]}`, status: http.StatusBadRequest, want: nameRequiredMessage},
		{name: "empty turns", method: http.MethodPost, path: evaluationDatasetPath("/evalds_test/cases"), body: `{"name":"one","turns":[]}`, status: http.StatusBadRequest, want: turnsRequiredMessage},
		{name: "import over limit", method: http.MethodPost, path: evaluationDatasetPath("/evalds_test/cases:import"), body: importOverLimitBody(), contentType: "application/x-ndjson", status: http.StatusBadRequest, want: importTooLargeMessage},
		{name: "bad jsonl", method: http.MethodPost, path: evaluationDatasetPath("/evalds_test/cases:import"), body: "{\"name\":\n", contentType: "application/jsonl", status: http.StatusBadRequest, want: importJSONLInvalidMessage},
		{name: "zip extra file", method: http.MethodPost, path: evaluationDatasetPath("/evalds_test/cases:import"), body: string(zipImport("cases.jsonl", "{\"name\":\"one\",\"turns\":[\"hello\"]}\n", "notes.txt")), contentType: "application/zip", status: http.StatusBadRequest, want: importZipInvalidMessage},
		{name: "other workspace", method: http.MethodGet, path: "/api/console/organizations/org_test/workspaces/other/evaluation-datasets/evalds_test", status: http.StatusNotFound, want: datasetNotFoundMessage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveEvaluation(router, test.method, test.path, test.body, test.contentType)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.want) || strings.Contains(response.Body.String(), `"type"`) {
				t.Fatalf("response = %s, want console error %q", response.Body.String(), test.want)
			}
		})
	}
	if store.createDatasetCalls != 0 || store.createCaseCalls != 0 || store.getCalls != 0 || store.listDatasetCalls != 0 {
		t.Fatalf("store calls = %+v, want none", store)
	}
}

func TestCreateEvaluationDatasetScopesPrincipalAndCreatesVersion(t *testing.T) {
	store := &evaluationStoreStub{}
	response := serveEvaluation(
		evaluationRouter(store),
		http.MethodPost,
		"/api/console/organizations/org_test/workspaces/default/evaluation-datasets",
		`{"name":" billing ","desc":" nightly "}`,
		"",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	created := store.createdDataset
	if created == nil || created.OrganizationUUID != "org_test" || created.WorkspaceUUID != "workspace-uuid" {
		t.Fatalf("created scope = %#v", created)
	}
	if created.Name != "billing" || created.Description != "nightly" || created.Version.Version != 1 || !strings.HasPrefix(created.Version.ExternalID, "edv_") {
		t.Fatalf("created dataset = %#v", created)
	}
	if !strings.HasPrefix(created.ExternalID, "evalds_") || !strings.Contains(response.Body.String(), `"type":"evaluation_dataset"`) || !strings.Contains(response.Body.String(), `"status":"available"`) {
		t.Fatalf("response = %s, created id = %q", response.Body.String(), created.ExternalID)
	}
}

func TestImportAndListEvaluationCases(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	store := &evaluationStoreStub{datasets: []db.EvaluationDataset{{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "evalds_test", Name: "billing",
		CreatedAt: now, UpdatedAt: now,
		Version: db.EvaluationDatasetVersion{UUID: "00000000-0000-4000-8000-000000000008", ExternalID: "edv_test", Version: 1, CreatedAt: now},
	}}}
	router := evaluationRouter(store)

	jsonl := "{\"name\":\" one \",\"turns\":[\" hello \"],\"desc\":\" note \",\"checkpoints\":[\"done\"]}\n\n"
	jsonResponse := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:import"), jsonl, "application/x-ndjson")
	if jsonResponse.Code != http.StatusOK || len(store.createdWrites) != 1 || store.createdWrites[0].Name != "one" || store.createdWrites[0].Turns[0] != "hello" {
		t.Fatalf("jsonl import = %d writes=%#v body=%s", jsonResponse.Code, store.createdWrites, jsonResponse.Body.String())
	}
	if store.caseWorkspace != "workspace-uuid" || !strings.HasPrefix(store.createdWrites[0].ExternalID, "evalcase_") || string(store.createdWrites[0].Checkpoints) != `["done"]` {
		t.Fatalf("import scope = %q write=%#v", store.caseWorkspace, store.createdWrites[0])
	}
	if !strings.Contains(jsonResponse.Body.String(), `"type":"evaluation_case"`) || !strings.Contains(jsonResponse.Body.String(), `"version_id":"edv_test"`) {
		t.Fatalf("jsonl import response = %s", jsonResponse.Body.String())
	}

	zipResponse := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:import"), string(zipImport("cases.jsonl", "{\"name\":\"second\",\"turns\":[\"hi\"]}\n", "")), "application/zip")
	if zipResponse.Code != http.StatusOK || len(store.createdWrites) != 1 || store.createdWrites[0].Name != "second" || len(store.createdWrites[0].Turns) != 1 {
		t.Fatalf("zip import = %d writes=%#v body=%s", zipResponse.Code, store.createdWrites, zipResponse.Body.String())
	}

	listDatasets := serveEvaluation(router, http.MethodGet, evaluationDatasetPath(""), "", "")
	if listDatasets.Code != http.StatusOK || store.lastDatasetList.WorkspaceUUID != "workspace-uuid" || !strings.Contains(listDatasets.Body.String(), `"id":"evalds_test"`) {
		t.Fatalf("list datasets = %d params=%#v body=%s", listDatasets.Code, store.lastDatasetList, listDatasets.Body.String())
	}
	listCases := serveEvaluation(router, http.MethodGet, evaluationDatasetPath("/evalds_test/cases"), "", "")
	if listCases.Code != http.StatusOK || store.lastCaseList.WorkspaceUUID != "workspace-uuid" || store.lastCaseList.DatasetExternalID != "evalds_test" {
		t.Fatalf("list cases = %d params=%#v body=%s", listCases.Code, store.lastCaseList, listCases.Body.String())
	}
	if !strings.Contains(listCases.Body.String(), `"turns":["hi"]`) {
		t.Fatalf("list cases body = %s", listCases.Body.String())
	}
}

func TestCreateEvaluationCaseReturnsSingleCase(t *testing.T) {
	store := &evaluationStoreStub{}
	response := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationDatasetPath("/evalds_test/cases"), `{"name":"one","turns":["hello"],"desc":"note"}`, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"type":"evaluation_case"`) || strings.Contains(response.Body.String(), `"data"`) {
		t.Fatalf("create case = %d %s", response.Code, response.Body.String())
	}
	if store.createCaseCalls != 1 || store.createdWrites[0].Turns[0] != "hello" {
		t.Fatalf("writes = %#v calls=%d", store.createdWrites, store.createCaseCalls)
	}
}

func evaluationRouter(store Store) http.Handler {
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", NewHandler(store, nil).RegisterRoutes)
	return router
}

func evaluationDatasetPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-datasets" + suffix
}

func serveEvaluation(router http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		OrganizationUUID:    "org_test",
		WorkspaceUUID:       "workspace-uuid",
		WorkspaceExternalID: "workspace_test",
		UserUUID:            "user-uuid",
	}))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func importOverLimitBody() string {
	return strings.Repeat("{\"name\":\"n\",\"turns\":[\"t\"]}\n", maxImportedCases+1)
}

func zipImport(name, jsonl, extra string) []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, _ := writer.Create(name)
	_, _ = file.Write([]byte(jsonl))
	if extra != "" {
		other, _ := writer.Create(extra)
		_, _ = other.Write([]byte("x"))
	}
	_ = writer.Close()
	return buffer.Bytes()
}

type evaluationStoreStub struct {
	createdDataset     *db.EvaluationDataset
	createDatasetCalls int
	datasets           []db.EvaluationDataset
	listDatasetCalls   int
	lastDatasetList    db.ListEvaluationDatasetsPageParams
	getCalls           int
	createCaseCalls    int
	createdWrites      []db.EvaluationCaseWrite
	caseWorkspace      string
	lastCaseList       db.ListEvaluationCasesPageParams
	listedCases        []db.EvaluationCase
}

func (s *evaluationStoreStub) CreateEvaluationDataset(_ context.Context, dataset db.EvaluationDataset) (db.EvaluationDataset, error) {
	s.createDatasetCalls++
	s.createdDataset = &dataset
	return dataset, nil
}

func (s *evaluationStoreStub) ListEvaluationDatasetsPage(_ context.Context, params db.ListEvaluationDatasetsPageParams) ([]db.EvaluationDataset, bool, error) {
	s.listDatasetCalls++
	s.lastDatasetList = params
	return s.datasets, false, nil
}

func (s *evaluationStoreStub) GetEvaluationDataset(_ context.Context, _, _, _ string) (db.EvaluationDataset, error) {
	s.getCalls++
	if len(s.datasets) == 0 {
		return db.EvaluationDataset{}, db.ErrNotFound
	}
	return s.datasets[0], nil
}

func (s *evaluationStoreStub) ListEvaluationCasesPage(_ context.Context, params db.ListEvaluationCasesPageParams) ([]db.EvaluationCase, bool, error) {
	s.lastCaseList = params
	if len(s.listedCases) > 0 {
		return s.listedCases, false, nil
	}
	return []db.EvaluationCase{{
		ExternalID: "evalcase_test", DatasetExternalID: params.DatasetExternalID, VersionExternalID: "edv_test",
		Position: 1, Name: "greeting", Turns: []string{"hello"},
		CreatedAt: time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC),
	}}, false, nil
}

func (s *evaluationStoreStub) CreateEvaluationCases(_ context.Context, _, workspaceUUID, datasetExternalID string, writes []db.EvaluationCaseWrite, now time.Time) ([]db.EvaluationCase, error) {
	s.createCaseCalls++
	s.createdWrites = append([]db.EvaluationCaseWrite(nil), writes...)
	s.caseWorkspace = workspaceUUID
	created := make([]db.EvaluationCase, 0, len(writes))
	for index, write := range writes {
		created = append(created, db.EvaluationCase{
			UUID: write.UUID, ExternalID: write.ExternalID, WorkspaceUUID: workspaceUUID,
			DatasetExternalID: datasetExternalID, VersionExternalID: "edv_test", Position: index + 1,
			Name: write.Name, Description: write.Description, Turns: write.Turns,
			Checkpoints: write.Checkpoints, InputFiles: write.InputFiles, ReferenceFiles: write.ReferenceFiles,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	s.listedCases = created
	return created, nil
}
