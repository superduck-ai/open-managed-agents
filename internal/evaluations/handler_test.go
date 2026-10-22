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

func TestEvaluationEvaluatorRoutesRejectInvalidInputBeforeStore(t *testing.T) {
	store := &evaluationStoreStub{}
	router := evaluationRouter(store)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{name: "empty name", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":" ","evaluator_type":"llm","model":"m","prompt_template":"{{input}}","variables":["input"]}`, want: nameRequiredMessage},
		{name: "bad type", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"human","variables":[]}`, want: evaluatorTypeInvalidMessage},
		{name: "llm without model", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"llm","prompt_template":"{{input}}","variables":["input"]}`, want: modelRequiredMessage},
		{name: "llm without variables", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"llm","model":"m","prompt_template":"score","variables":[]}`, want: variablesRequiredMessage},
		{name: "unknown variable", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"llm","model":"m","prompt_template":"{{ideal}}","variables":["ideal"]}`, want: variablesInvalidMessage},
		{name: "unselected placeholder", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"llm","model":"m","prompt_template":"{{output}}","variables":["input"]}`, want: promptVariableMessage},
		{name: "agent without id", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"agent","variables":[]}`, want: agentIDRequiredMessage},
		{name: "agent with prompt", method: http.MethodPost, path: evaluationEvaluatorPath(""), body: `{"name":"score","evaluator_type":"agent","agent_id":"agent_score","prompt_template":"{{input}}","variables":["input"]}`, want: promptForbiddenMessage},
		{name: "other workspace", method: http.MethodGet, path: "/api/console/organizations/org_test/workspaces/other/evaluation-evaluators/evalev_test", want: evaluatorNotFoundMessage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveEvaluation(router, test.method, test.path, test.body, "")
			if response.Code != http.StatusBadRequest && test.name != "other workspace" {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			if test.name == "other workspace" && response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.want) || strings.Contains(response.Body.String(), `"type"`) {
				t.Fatalf("response = %s, want console error %q", response.Body.String(), test.want)
			}
		})
	}
	if store.createEvaluatorCalls != 0 || store.getEvaluatorCalls != 0 || store.listEvaluatorCalls != 0 {
		t.Fatalf("store calls = %+v, want none", store)
	}
}

func TestCreateAndListEvaluationEvaluators(t *testing.T) {
	store := &evaluationStoreStub{}
	router := evaluationRouter(store)
	llmBody := `{"name":" score ","desc":" nightly ","evaluator_type":"llm","model":" claude ","prompt_template":"Score {{output}} against {{input}}","variables":["output","input"]}`
	llmResponse := serveEvaluation(router, http.MethodPost, "/api/console/organizations/org_test/workspaces/default/evaluation-evaluators", llmBody, "")
	created := store.createdEvaluator
	if llmResponse.Code != http.StatusOK || created == nil || created.WorkspaceUUID != "workspace-uuid" || created.Name != "score" || created.Description != "nightly" {
		t.Fatalf("llm create = %d %#v body=%s", llmResponse.Code, created, llmResponse.Body.String())
	}
	if created.Version.Version != 1 || created.Version.EvaluatorType != "llm" || created.Version.Model != "claude" || created.Version.AgentID != "" {
		t.Fatalf("llm version = %#v", created.Version)
	}
	if len(created.Version.Variables) != 2 || created.Version.Variables[0] != "input" || created.Version.Variables[1] != "output" || !strings.HasPrefix(created.Version.ExternalID, "eev_") {
		t.Fatalf("llm variables = %#v", created.Version)
	}
	if !strings.HasPrefix(created.ExternalID, "evalev_") || strings.Contains(llmResponse.Body.String(), "agent_contract") || !strings.Contains(llmResponse.Body.String(), `"type":"evaluation_evaluator"`) {
		t.Fatalf("llm response = %s", llmResponse.Body.String())
	}

	agentBody := `{"name":"judge","evaluator_type":"agent","agent_id":" agent_score ","variables":["invocation_result"]}`
	agentResponse := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), agentBody, "")
	created = store.createdEvaluator
	if agentResponse.Code != http.StatusOK || created.Version.EvaluatorType != "agent" || created.Version.AgentID != "agent_score" || created.Version.Model != "" || created.Version.PromptTemplate != "" {
		t.Fatalf("agent create = %d %#v body=%s", agentResponse.Code, created, agentResponse.Body.String())
	}
	if !strings.Contains(agentResponse.Body.String(), `"result_path":"/data/session/result.json"`) || !strings.Contains(agentResponse.Body.String(), `"score_path":"/output/score.json"`) || !strings.Contains(agentResponse.Body.String(), `"score_fields":["score","explanation","details"]`) {
		t.Fatalf("agent response = %s", agentResponse.Body.String())
	}

	store.evaluators = []db.EvaluationEvaluator{*created}
	listResponse := serveEvaluation(router, http.MethodGet, evaluationEvaluatorPath(""), "", "")
	if listResponse.Code != http.StatusOK || store.lastEvaluatorList.WorkspaceUUID != "workspace-uuid" || !strings.Contains(listResponse.Body.String(), `"id":"`+created.ExternalID+`"`) {
		t.Fatalf("list = %d params=%#v body=%s", listResponse.Code, store.lastEvaluatorList, listResponse.Body.String())
	}
	getResponse := serveEvaluation(router, http.MethodGet, evaluationEvaluatorPath("/"+created.ExternalID), "", "")
	if getResponse.Code != http.StatusOK || store.getEvaluatorCalls != 1 || !strings.Contains(getResponse.Body.String(), `"evaluator_type":"agent"`) {
		t.Fatalf("get = %d calls=%d body=%s", getResponse.Code, store.getEvaluatorCalls, getResponse.Body.String())
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

func TestEvaluationZipImportAllowsCompanionFiles(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	store := &evaluationStoreStub{datasets: []db.EvaluationDataset{{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "evalds_test", Name: "billing",
		CreatedAt: now, UpdatedAt: now,
		Version: db.EvaluationDatasetVersion{UUID: "00000000-0000-4000-8000-000000000008", ExternalID: "edv_test", Version: 1, CreatedAt: now},
	}}}
	body := zipMembers(map[string]string{
		"pack/cases.jsonl":              "{\"name\":\"packed\",\"turns\":[\"hi\"]}\n",
		"pack/input_files/brief.txt":    "brief",
		"pack/reference_files/gold.txt": "gold",
		"pack/.DS_Store":                "x",
		"pack/__MACOSX/cases.jsonl":     "{\"name\":\"hidden\",\"turns\":[\"no\"]}\n",
	})
	response := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationDatasetPath("/evalds_test/cases:import"), string(body), "application/zip")
	if response.Code != http.StatusOK || len(store.createdWrites) != 1 || store.createdWrites[0].Name != "packed" {
		t.Fatalf("companion zip = %d writes=%#v body=%s", response.Code, store.createdWrites, response.Body.String())
	}
	if _, ok := zipMemberPath("../notes.txt"); ok {
		t.Fatal("zip member path accepted traversal")
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
	return evaluationRouterWith(NewHandler(store, nil))
}

func evaluationRouterWith(handler *Handler) http.Handler {
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)
	return router
}

func evaluationDatasetPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-datasets" + suffix
}

func evaluationEvaluatorPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-evaluators" + suffix
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
	members := map[string]string{name: jsonl}
	if extra != "" {
		members[extra] = "x"
	}
	return zipMembers(members)
}

func zipMembers(members map[string]string) []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range members {
		file, _ := writer.Create(name)
		_, _ = file.Write([]byte(contents))
	}
	_ = writer.Close()
	return buffer.Bytes()
}

type evaluationStoreStub struct {
	createdDataset        *db.EvaluationDataset
	createDatasetCalls    int
	datasets              []db.EvaluationDataset
	listDatasetCalls      int
	lastDatasetList       db.ListEvaluationDatasetsPageParams
	getCalls              int
	createCaseCalls       int
	createdWrites         []db.EvaluationCaseWrite
	caseWorkspace         string
	lastCaseList          db.ListEvaluationCasesPageParams
	listedCases           []db.EvaluationCase
	createdEvaluator      *db.EvaluationEvaluator
	createEvaluatorCalls  int
	evaluators            []db.EvaluationEvaluator
	listEvaluatorCalls    int
	lastEvaluatorList     db.ListEvaluationEvaluatorsPageParams
	getEvaluatorCalls     int
	version               db.EvaluationDatasetVersion
	versionCases          []db.EvaluationCase
	agent                 db.Agent
	agentVersion          db.Agent
	frozenAgents          []db.Agent
	environment           db.Environment
	createdExperiment     *db.EvaluationExperimentCreate
	experiment            db.EvaluationExperiment
	experiments           []db.EvaluationExperiment
	completedExperiments  []db.EvaluationExperiment
	createExperimentErr   error
	createExperimentCalls int
	getExperimentCalls    int
	listExperimentCalls   int
	compareCalls          int
	lastCompareVersion    string
	lastCompareKey        string
	updateStatusCalls     int
	lastStatus            string
	itemSessionUpdates    []db.EvaluationExperimentItemSession
	evaluatorVersions     []db.EvaluationEvaluatorVersion
	sessionEvents         []db.SessionEvent
	sessions              []db.Session
	resources             []db.SessionResource
	getSessionCalls       int
	listResourceCalls     int
	getVersionCalls       int
	listVersionCaseCalls  int
	getAgentCalls         int
	getEnvironmentCalls   int
}

func (s *evaluationStoreStub) CreateEvaluationDataset(_ context.Context, dataset db.EvaluationDataset) (db.EvaluationDataset, error) {
	s.createDatasetCalls++
	s.createdDataset = &dataset
	return dataset, nil
}

func (s *evaluationStoreStub) CreateEvaluationDatasetWithCases(_ context.Context, dataset db.EvaluationDataset, writes []db.EvaluationCaseWrite) (db.EvaluationDataset, error) {
	s.createDatasetCalls++
	s.createdDataset = &dataset
	s.createdWrites = append([]db.EvaluationCaseWrite(nil), writes...)
	dataset.Version.CaseCount = len(writes)
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

func (s *evaluationStoreStub) CreateEvaluationEvaluator(_ context.Context, evaluator db.EvaluationEvaluator) (db.EvaluationEvaluator, error) {
	s.createEvaluatorCalls++
	s.createdEvaluator = &evaluator
	return evaluator, nil
}

func (s *evaluationStoreStub) ListEvaluationEvaluatorsPage(_ context.Context, params db.ListEvaluationEvaluatorsPageParams) ([]db.EvaluationEvaluator, bool, error) {
	s.listEvaluatorCalls++
	s.lastEvaluatorList = params
	return s.evaluators, false, nil
}

func (s *evaluationStoreStub) GetEvaluationEvaluator(_ context.Context, _, _, externalID string) (db.EvaluationEvaluator, error) {
	s.getEvaluatorCalls++
	for _, evaluator := range s.evaluators {
		if evaluator.ExternalID == externalID {
			return evaluator, nil
		}
	}
	return db.EvaluationEvaluator{}, db.ErrNotFound
}

func (s *evaluationStoreStub) GetEvaluationDatasetVersion(_ context.Context, _, _, externalID string) (db.EvaluationDatasetVersion, error) {
	s.getVersionCalls++
	if s.version.ExternalID == "" || s.version.ExternalID != externalID {
		return db.EvaluationDatasetVersion{}, db.ErrNotFound
	}
	return s.version, nil
}

func (s *evaluationStoreStub) ListEvaluationCasesByVersion(_ context.Context, _, _, _ string) ([]db.EvaluationCase, error) {
	s.listVersionCaseCalls++
	return append([]db.EvaluationCase(nil), s.versionCases...), nil
}

func (s *evaluationStoreStub) GetAgent(_ context.Context, _, externalID string) (db.Agent, error) {
	s.getAgentCalls++
	if s.agent.ExternalID == "" || s.agent.ExternalID != externalID {
		return db.Agent{}, db.ErrNotFound
	}
	return s.agent, nil
}

func (s *evaluationStoreStub) GetAgentVersion(_ context.Context, _, externalID string, version int) (db.Agent, error) {
	s.getAgentCalls++
	for _, agent := range s.frozenAgents {
		if agent.ExternalID == externalID && agent.CurrentVersion == version {
			return agent, nil
		}
	}
	if s.agentVersion.ExternalID == "" || s.agentVersion.ExternalID != externalID || s.agentVersion.CurrentVersion != version {
		return db.Agent{}, db.ErrNotFound
	}
	return s.agentVersion, nil
}

func (s *evaluationStoreStub) GetEnvironment(_ context.Context, _, externalID string) (db.Environment, error) {
	s.getEnvironmentCalls++
	if s.environment.ExternalID == "" || s.environment.ExternalID != externalID {
		return db.Environment{}, db.ErrNotFound
	}
	return s.environment, nil
}

func (s *evaluationStoreStub) CreateEvaluationExperiment(_ context.Context, input db.EvaluationExperimentCreate) (db.EvaluationExperiment, error) {
	s.createExperimentCalls++
	if s.createExperimentErr != nil {
		return db.EvaluationExperiment{}, s.createExperimentErr
	}
	copied := input
	s.createdExperiment = &copied
	created := input.Experiment
	created.Bindings = append([]db.EvaluationExperimentBinding(nil), input.Bindings...)
	created.Items = append([]db.EvaluationExperimentItem(nil), input.Items...)
	created.ItemCount = int64(len(created.Items))
	created.EvaluatorCount = int64(len(created.Bindings))
	s.experiment = created
	return created, nil
}

func (s *evaluationStoreStub) GetEvaluationExperiment(_ context.Context, _, _, externalID string) (db.EvaluationExperiment, error) {
	s.getExperimentCalls++
	if s.experiment.ExternalID == "" || s.experiment.ExternalID != externalID {
		return db.EvaluationExperiment{}, db.ErrNotFound
	}
	return s.experiment, nil
}

func (s *evaluationStoreStub) ListEvaluationExperimentsPage(_ context.Context, params db.ListEvaluationExperimentsPageParams) ([]db.EvaluationExperiment, bool, error) {
	s.listExperimentCalls++
	if params.WorkspaceUUID != "workspace-uuid" {
		return nil, false, nil
	}
	return append([]db.EvaluationExperiment(nil), s.experiments...), false, nil
}

func (s *evaluationStoreStub) ListCompletedEvaluationExperiments(_ context.Context, _, _, versionExternalID, evaluatorSetKey string) ([]db.EvaluationExperiment, error) {
	s.compareCalls++
	s.lastCompareVersion = versionExternalID
	s.lastCompareKey = evaluatorSetKey
	return append([]db.EvaluationExperiment(nil), s.completedExperiments...), nil
}

func (s *evaluationStoreStub) UpdateEvaluationExperimentStatus(_ context.Context, _, _, externalID, status string, updatedAt time.Time) error {
	s.updateStatusCalls++
	s.lastStatus = status
	if s.experiment.ExternalID == externalID {
		s.experiment.Status = status
		s.experiment.UpdatedAt = updatedAt
	}
	return nil
}

func (s *evaluationStoreStub) ListRunningEvaluationExperimentItemsBySession(_ context.Context, organizationUUID, workspaceUUID, sessionExternalID string) ([]db.EvaluationExperimentSessionMatch, error) {
	matches := make([]db.EvaluationExperimentSessionMatch, 0)
	if s.experiment.OrganizationUUID != organizationUUID || s.experiment.WorkspaceUUID != workspaceUUID {
		return matches, nil
	}
	for _, item := range s.experiment.Items {
		if item.Status == db.EvaluationExperimentStatusRunning && item.SessionExternalID == sessionExternalID {
			matches = append(matches, db.EvaluationExperimentSessionMatch{Item: item, ExperimentExternalID: s.experiment.ExternalID})
		}
	}
	return matches, nil
}

func (s *evaluationStoreStub) GetEvaluationEvaluatorVersion(_ context.Context, _, _, externalID string) (db.EvaluationEvaluatorVersion, error) {
	for _, version := range s.evaluatorVersions {
		if version.ExternalID == externalID {
			return version, nil
		}
	}
	return db.EvaluationEvaluatorVersion{}, db.ErrNotFound
}

func (s *evaluationStoreStub) CreateEvaluationExperimentScore(_ context.Context, score db.EvaluationExperimentScore) (db.EvaluationExperimentScore, error) {
	for _, existing := range s.experiment.Scores {
		if existing.ItemUUID == score.ItemUUID && existing.VersionUUID == score.VersionUUID {
			return db.EvaluationExperimentScore{}, db.ErrDuplicate
		}
	}
	if score.UpdatedAt.IsZero() {
		score.UpdatedAt = score.CreatedAt
	}
	s.experiment.Scores = append(s.experiment.Scores, score)
	return score, nil
}

func (s *evaluationStoreStub) ListSessionEventsPage(_ context.Context, params db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	events := make([]db.SessionEvent, 0, len(s.sessionEvents))
	for _, event := range s.sessionEvents {
		if event.SessionExternalID != "" && event.SessionExternalID != params.SessionExternalID {
			continue
		}
		events = append(events, event)
	}
	return events, false, nil
}

func (s *evaluationStoreStub) GetSession(_ context.Context, workspaceUUID, externalID string) (db.Session, bool, error) {
	s.getSessionCalls++
	if workspaceUUID != "workspace-uuid" {
		return db.Session{}, false, nil
	}
	for _, session := range s.sessions {
		if session.ExternalID == externalID {
			return session, true, nil
		}
	}
	return db.Session{}, false, nil
}

func (s *evaluationStoreStub) ListSessionResources(_ context.Context, workspaceUUID, sessionExternalID string) ([]db.SessionResource, error) {
	s.listResourceCalls++
	if workspaceUUID != "workspace-uuid" {
		return nil, nil
	}
	resources := make([]db.SessionResource, 0)
	for _, resource := range s.resources {
		if resource.SessionExternalID == sessionExternalID {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func (s *evaluationStoreStub) FinishRunningEvaluationExperimentItem(_ context.Context, _, _, externalID, status, errorMessage string, updatedAt time.Time) (bool, error) {
	for index := range s.experiment.Items {
		item := &s.experiment.Items[index]
		if item.ExternalID != externalID || item.Status != db.EvaluationExperimentStatusRunning {
			continue
		}
		item.Status = status
		item.ErrorMessage = errorMessage
		item.UpdatedAt = updatedAt
		return true, nil
	}
	return false, nil
}

func (s *evaluationStoreStub) SetEvaluationExperimentItemSession(_ context.Context, update db.EvaluationExperimentItemSession) error {
	s.itemSessionUpdates = append(s.itemSessionUpdates, update)
	for index := range s.experiment.Items {
		if s.experiment.Items[index].ExternalID != update.ExternalID {
			continue
		}
		s.experiment.Items[index].Status = update.Status
		s.experiment.Items[index].SessionExternalID = update.SessionExternalID
		s.experiment.Items[index].ErrorMessage = update.ErrorMessage
		s.experiment.Items[index].UpdatedAt = update.UpdatedAt
	}
	sessions := 0
	for _, item := range s.experiment.Items {
		if item.SessionExternalID != "" {
			sessions++
		}
	}
	s.experiment.SessionCount = int64(sessions)
	return nil
}
