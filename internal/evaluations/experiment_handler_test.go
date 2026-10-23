package evaluations

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestEvaluationExperimentRoutesRejectInvalidInputBeforeStore(t *testing.T) {
	store := &evaluationStoreStub{}
	router := evaluationRouter(store)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{name: "empty name", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":" ","dataset_version_id":"edv_test","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_test"]}`, status: http.StatusBadRequest, want: nameRequiredMessage},
		{name: "missing version", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":"nightly","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_test"]}`, status: http.StatusBadRequest, want: datasetVersionRequiredMessage},
		{name: "no evaluators", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":60,"evaluator_ids":[]}`, status: http.StatusBadRequest, want: evaluatorIDsRequiredMessage},
		{name: "duplicate evaluators", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_test","evalev_test"]}`, status: http.StatusBadRequest, want: evaluatorIDsDuplicateMessage},
		{name: "bad timeout", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":0,"evaluator_ids":["evalev_test"]}`, status: http.StatusBadRequest, want: timeoutInvalidMessage},
		{name: "bad env", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_test"],"env":["NO"]}`, status: http.StatusBadRequest, want: envInvalidMessage},
		{name: "bad resources", method: http.MethodPost, path: evaluationExperimentPath(""), body: `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_test","environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_test"],"resources":{}}`, status: http.StatusBadRequest, want: resourcesInvalidMessage},
		{name: "other workspace", method: http.MethodGet, path: "/api/console/organizations/org_test/workspaces/other/evaluation-experiments/evalexp_test", status: http.StatusNotFound, want: experimentNotFoundMessage},
		{name: "compare without evaluators", method: http.MethodPost, path: evaluationExperimentPath(":compare"), body: `{"dataset_version_id":"edv_test","evaluator_ids":[]}`, status: http.StatusBadRequest, want: evaluatorIDsRequiredMessage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveEvaluation(router, test.method, test.path, test.body, "")
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("response = %d %s, want %d %q", response.Code, response.Body.String(), test.status, test.want)
			}
		})
	}
	if store.createExperimentCalls != 0 || store.getVersionCalls != 0 || store.getAgentCalls != 0 {
		t.Fatalf("store calls = %+v, want none", store)
	}
}

func TestCreateExperimentFreezesBindingsAndStartsSessions(t *testing.T) {
	store := readyExperimentStore()
	starter := &evaluationSessionStarterStub{sessionID: "sesn_case"}
	handler := NewHandler(store, nil).WithSessionStarter(starter)
	response := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	created := store.createdExperiment
	if response.Code != http.StatusOK || created == nil {
		t.Fatalf("create = %d %#v body=%s", response.Code, created, response.Body.String())
	}
	if created.Experiment.WorkspaceUUID != "workspace-uuid" || created.Experiment.Name != "nightly" || created.Experiment.AgentVersion != 4 {
		t.Fatalf("experiment = %#v", created.Experiment)
	}
	if created.Experiment.VersionExternalID != "edv_test" || created.Experiment.DatasetExternalID != "evalds_test" || created.Experiment.EvaluatorSetKey != "eev_a|eev_b" {
		t.Fatalf("freeze = %#v", created.Experiment)
	}
	if len(created.Bindings) != 2 || created.Bindings[0].VersionExternalID != "eev_b" || created.Bindings[1].EvaluatorExternalID != "evalev_a" {
		t.Fatalf("bindings = %#v", created.Bindings)
	}
	if len(created.Items) != 1 || created.Items[0].Status != db.EvaluationExperimentStatusPending || created.Items[0].CaseExternalID != "evalcase_test" || !strings.HasPrefix(created.Items[0].ExternalID, "evalitem_") {
		t.Fatalf("items = %#v", created.Items)
	}
	if starter.calls != 1 || starter.last.AgentVersion != 4 || starter.last.Turns[0] != "hello" || starter.last.Metadata["case_id"] != "evalcase_test" || string(starter.last.Resources) != `[{"type":"file"}]` || starter.last.TimeoutSeconds != 60 || string(starter.last.Env) != `{"TRACE":"1"}` {
		t.Fatalf("starter = %#v", starter)
	}
	if store.lastStatus != db.EvaluationExperimentStatusRunning || len(store.itemSessionUpdates) != 1 || store.itemSessionUpdates[0].SessionExternalID != "sesn_case" {
		t.Fatalf("status = %s updates=%#v", store.lastStatus, store.itemSessionUpdates)
	}
	if !strings.Contains(response.Body.String(), `"session_id":"sesn_case"`) || !strings.Contains(response.Body.String(), `"status":"running"`) || !strings.HasPrefix(created.Experiment.ExternalID, "evalexp_") {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestCreateExperimentOmitsEnvAndStillStartsSession(t *testing.T) {
	store := readyExperimentStore()
	starter := &evaluationSessionStarterStub{sessionID: "sesn_case"}
	response := serveEvaluation(evaluationRouterWith(NewHandler(store, nil).WithSessionStarter(starter)), http.MethodPost, evaluationExperimentPath(""), `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_b"]}`, "")
	if response.Code != http.StatusOK || starter.calls != 1 || starter.last.TimeoutSeconds != 60 || len(starter.last.Env) != 0 || starter.last.AgentVersion != 4 {
		t.Fatalf("start = %d starter=%#v body=%s", response.Code, starter, response.Body.String())
	}
}

func TestCreateExperimentRejectsMissingScorerAgent(t *testing.T) {
	store := readyExperimentStore()
	store.evaluators[1].Version.EvaluatorType = evaluatorTypeAgent
	store.evaluators[1].Version.AgentID = "agent_score"
	response := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_a"]}`, "")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), agentNotFoundMessage) || store.createExperimentCalls != 0 {
		t.Fatalf("missing scorer = %d calls=%d body=%s", response.Code, store.createExperimentCalls, response.Body.String())
	}
}

func TestCreateExperimentLeavesPendingItemsWithoutStarter(t *testing.T) {
	store := readyExperimentStore()
	response := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if response.Code != http.StatusOK || store.experiment.Status != db.EvaluationExperimentStatusPending || store.updateStatusCalls != 0 {
		t.Fatalf("create = %d status=%s body=%s", response.Code, store.experiment.Status, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"session_id"`) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestStartExperimentRecordsSessionFailure(t *testing.T) {
	store := readyExperimentStore()
	created := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	starter := &evaluationSessionStarterStub{err: apperr.New(apperr.InvalidArgument, "environment must not be archived", nil)}
	response := serveEvaluation(evaluationRouterWith(NewHandler(store, nil).WithSessionStarter(starter)), http.MethodPost, evaluationExperimentPath("/"+store.experiment.ExternalID+":start"), "", "")
	if response.Code != http.StatusOK || store.lastStatus != db.EvaluationExperimentStatusFailed || store.itemSessionUpdates[0].ErrorMessage != "environment must not be archived" {
		t.Fatalf("start = %d status=%s updates=%#v body=%s", response.Code, store.lastStatus, store.itemSessionUpdates, response.Body.String())
	}
}

func TestCompareExperimentsReturnsOnlyCompletedMatches(t *testing.T) {
	store := readyExperimentStore()
	score := 1.0
	store.completedExperiments = []db.EvaluationExperiment{{
		ExternalID: "evalexp_done", Name: "done", Status: db.EvaluationExperimentStatusCompleted,
		DatasetExternalID: "evalds_test", VersionExternalID: "edv_test", AgentExternalID: "agent_other", AgentVersion: 2,
		EnvironmentExternalID: "env_test", TimeoutSeconds: 60, CreatedAt: time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC), UpdatedAt: time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC),
		Bindings: []db.EvaluationExperimentBinding{{EvaluatorExternalID: "evalev_a", VersionExternalID: "eev_a", Position: 1}},
		Scores:   []db.EvaluationExperimentScore{{ExternalID: "evalscore_done", ItemExternalID: "evalitem_done", VersionExternalID: "eev_a", Status: db.EvaluationExperimentScoreStatusCompleted, Score: &score}},
	}}
	empty := serveEvaluation(evaluationRouter(&evaluationStoreStub{version: store.version, evaluators: store.evaluators}), http.MethodPost, evaluationExperimentPath(":compare"), `{"dataset_version_id":"edv_test","evaluator_ids":["evalev_b","evalev_a"]}`, "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"data":[]`) {
		t.Fatalf("empty compare = %d %s", empty.Code, empty.Body.String())
	}
	response := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(":compare"), `{"dataset_version_id":"edv_test","evaluator_ids":["evalev_b","evalev_a"]}`, "")
	if response.Code != http.StatusOK || store.lastCompareKey != "eev_a|eev_b" || store.lastCompareVersion != "edv_test" {
		t.Fatalf("compare = %d key=%s version=%s body=%s", response.Code, store.lastCompareKey, store.lastCompareVersion, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"id":"evalexp_done"`) || !strings.Contains(response.Body.String(), `"agent_version":2`) || !strings.Contains(response.Body.String(), `"id":"evalscore_done"`) || strings.Contains(response.Body.String(), `"items"`) {
		t.Fatalf("compare body = %s", response.Body.String())
	}
}

func TestListExperimentsOmitsItems(t *testing.T) {
	store := readyExperimentStore()
	score := 0.5
	store.experiments = []db.EvaluationExperiment{{
		ExternalID: "evalexp_list", Name: "nightly", Status: db.EvaluationExperimentStatusPending,
		DatasetExternalID: "evalds_test", VersionExternalID: "edv_test", AgentExternalID: "agent_test", AgentVersion: 4,
		EnvironmentExternalID: "env_test", TimeoutSeconds: 60, ItemCount: 1, EvaluatorCount: 2,
		CreatedAt: time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC), UpdatedAt: time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC),
		Items: []db.EvaluationExperimentItem{{ExternalID: "evalitem_hidden", CaseExternalID: "evalcase_test", Status: db.EvaluationExperimentStatusPending}},
		Scores: []db.EvaluationExperimentScore{{
			ExternalID: "evalscore_list", ItemExternalID: "evalitem_scored", VersionExternalID: "eev_a",
			Status: db.EvaluationExperimentScoreStatusCompleted, Score: &score,
		}},
	}}
	response := serveEvaluation(evaluationRouter(store), http.MethodGet, evaluationExperimentPath(""), "", "")
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"item_count":1`) || strings.Contains(body, `"items"`) || strings.Contains(body, "evalitem_hidden") || !strings.Contains(body, `"id":"evalscore_list"`) || !strings.Contains(body, `"score":0.5`) {
		t.Fatalf("list = %d %s", response.Code, body)
	}
}

func TestCreateExperimentRejectsMissingVersionAndDuplicateName(t *testing.T) {
	store := readyExperimentStore()
	store.version = db.EvaluationDatasetVersion{}
	missing := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), datasetVersionNotFoundMessage) || store.createExperimentCalls != 0 {
		t.Fatalf("missing version = %d calls=%d body=%s", missing.Code, store.createExperimentCalls, missing.Body.String())
	}

	store = readyExperimentStore()
	store.versionCases = nil
	emptyCases := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if emptyCases.Code != http.StatusBadRequest || !strings.Contains(emptyCases.Body.String(), casesRequiredMessage) || store.getAgentCalls != 0 {
		t.Fatalf("empty cases = %d agentCalls=%d body=%s", emptyCases.Code, store.getAgentCalls, emptyCases.Body.String())
	}

	archived := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	store = readyExperimentStore()
	store.environment.ArchivedAt = &archived
	archivedEnvironment := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if archivedEnvironment.Code != http.StatusBadRequest || !strings.Contains(archivedEnvironment.Body.String(), environmentArchivedMessage) || store.createExperimentCalls != 0 {
		t.Fatalf("archived environment = %d calls=%d body=%s", archivedEnvironment.Code, store.createExperimentCalls, archivedEnvironment.Body.String())
	}

	store = readyExperimentStore()
	store.createExperimentErr = db.ErrDuplicate
	duplicate := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), duplicateResourceMessage) {
		t.Fatalf("duplicate = %d %s", duplicate.Code, duplicate.Body.String())
	}
}

func evaluationExperimentPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-experiments" + suffix
}

func experimentCreateBody() string {
	return `{"name":" nightly ","desc":" note ","dataset_version_id":" edv_test ","agent_id":" agent_pinned ","agent_version":4,"environment_id":" env_test ","timeout_seconds":60,"env":{"TRACE":"1"},"resources":[{"type":"file"}],"evaluator_ids":[" evalev_b ","evalev_a"]}`
}

func readyExperimentStore() *evaluationStoreStub {
	return &evaluationStoreStub{
		version: db.EvaluationDatasetVersion{
			UUID: "00000000-0000-4000-8000-000000000008", ExternalID: "edv_test",
			DatasetUUID: "00000000-0000-4000-8000-000000000003", DatasetExternalID: "evalds_test", Version: 1,
		},
		versionCases: []db.EvaluationCase{{
			UUID: "00000000-0000-4000-8000-000000000009", ExternalID: "evalcase_test", Position: 1, Turns: []string{"hello"},
		}},
		agentVersion: db.Agent{ExternalID: "agent_pinned", CurrentVersion: 4},
		environment:  db.Environment{ExternalID: "env_test"},
		evaluators: []db.EvaluationEvaluator{
			{UUID: "00000000-0000-4000-8000-000000000021", ExternalID: "evalev_b", Version: db.EvaluationEvaluatorVersion{UUID: "00000000-0000-4000-8000-000000000031", ExternalID: "eev_b", Version: 1}},
			{UUID: "00000000-0000-4000-8000-000000000022", ExternalID: "evalev_a", Version: db.EvaluationEvaluatorVersion{UUID: "00000000-0000-4000-8000-000000000032", ExternalID: "eev_a", Version: 1}},
		},
	}
}

type evaluationSessionStarterStub struct {
	sessionID string
	err       error
	calls     int
	last      CaseSessionInput
}

func (s *evaluationSessionStarterStub) StartCaseSession(_ context.Context, _ auth.Principal, input CaseSessionInput) (string, error) {
	s.calls++
	s.last = input
	if s.err != nil {
		return "", s.err
	}
	return s.sessionID, nil
}
