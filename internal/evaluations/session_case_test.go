package evaluations

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCreateCaseFromSessionRejectsUnusableSources(t *testing.T) {
	store := &evaluationStoreStub{datasets: []db.EvaluationDataset{{ExternalID: "evalds_test"}}}
	router := evaluationRouter(store)
	other := serveEvaluation(router, http.MethodPost, "/api/console/organizations/org_test/workspaces/other/evaluation-datasets/evalds_test/cases:from-session", `{"session_id":"sesn_case"}`, "")
	if other.Code != http.StatusNotFound || !strings.Contains(other.Body.String(), datasetNotFoundMessage) || store.getCalls != 0 || store.getSessionCalls != 0 || store.createCaseCalls != 0 {
		t.Fatalf("other workspace = %d calls=%d %s", other.Code, store.createCaseCalls, other.Body.String())
	}
	missingID := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{}`, "")
	if missingID.Code != http.StatusBadRequest || !strings.Contains(missingID.Body.String(), sessionIDRequiredMessage) || store.getCalls != 0 {
		t.Fatalf("missing id = %d calls=%d %s", missingID.Code, store.getCalls, missingID.Body.String())
	}
	missingSession := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{"session_id":"sesn_missing"}`, "")
	if missingSession.Code != http.StatusNotFound || !strings.Contains(missingSession.Body.String(), sessionNotFoundMessage) || store.createCaseCalls != 0 {
		t.Fatalf("missing session = %d calls=%d %s", missingSession.Code, store.createCaseCalls, missingSession.Body.String())
	}
	store.sessions = []db.Session{{
		ExternalID: "sesn_score",
		Metadata:   json.RawMessage(`{"evaluation_role":"agent_score"}`),
	}}
	store.sessionEvents = []db.SessionEvent{{EventType: "user.message", Payload: json.RawMessage(`{"text":"grade this"}`)}}
	scoring := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{"session_id":"sesn_score"}`, "")
	if scoring.Code != http.StatusBadRequest || !strings.Contains(scoring.Body.String(), scoringSessionMessage) || store.createCaseCalls != 0 {
		t.Fatalf("scoring = %d calls=%d %s", scoring.Code, store.createCaseCalls, scoring.Body.String())
	}
	store.sessions = []db.Session{{ExternalID: "sesn_empty"}}
	store.sessionEvents = []db.SessionEvent{{EventType: "agent.message", Payload: json.RawMessage(`{"text":"only the agent spoke"}`)}}
	empty := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{"session_id":" sesn_empty "}`, "")
	if empty.Code != http.StatusBadRequest || !strings.Contains(empty.Body.String(), turnsRequiredMessage) || store.createCaseCalls != 0 {
		t.Fatalf("empty turns = %d calls=%d %s", empty.Code, store.createCaseCalls, empty.Body.String())
	}
	archivedAt := time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
	store.datasets[0].ArchivedAt = &archivedAt
	sessionsBefore := store.getSessionCalls
	archived := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{"session_id":"sesn_empty"}`, "")
	if archived.Code != http.StatusBadRequest || !strings.Contains(archived.Body.String(), datasetArchivedMessage) || store.getSessionCalls != sessionsBefore || store.createCaseCalls != 0 {
		t.Fatalf("archived = %d sessionCalls=%d %s", archived.Code, store.getSessionCalls, archived.Body.String())
	}
}

func TestCreateCaseFromSessionCopiesUserTurns(t *testing.T) {
	title := "Bad checkout"
	store := &evaluationStoreStub{
		datasets: []db.EvaluationDataset{{ExternalID: "evalds_test"}},
		sessions: []db.Session{{ExternalID: "sesn_case", Title: &title}},
		sessionEvents: []db.SessionEvent{
			{EventType: "user.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"where is my order"}]}`)},
			{EventType: "agent.message", Payload: json.RawMessage(`{"text":"I refunded it"}`)},
			{EventType: "user.message", Payload: json.RawMessage(`{"text":"it failed"}`)},
			{EventType: "agent.tool_result", Payload: json.RawMessage(`{"text":"tool output"}`)},
		},
		resources: []db.SessionResource{
			{
				SessionExternalID: "sesn_case",
				ResourceType:      "file",
				Payload:           json.RawMessage(`{"file_id":"file_in","mount_path":"/uploads/brief.txt"}`),
			},
			{
				SessionExternalID: "sesn_case",
				ResourceType:      "file",
				Path:              "/outputs/report.txt",
				FileExternalID:    "file_out",
			},
			{SessionExternalID: "sesn_case", ResourceType: "memory_store"},
		},
	}
	response := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{"session_id":"sesn_case","desc":"from console","checkpoints":["refunded"]}`, "")
	if response.Code != http.StatusOK || store.createCaseCalls != 1 || store.createDatasetCalls != 0 {
		t.Fatalf("create = %d calls=%d datasets=%d %s", response.Code, store.createCaseCalls, store.createDatasetCalls, response.Body.String())
	}
	write := store.createdWrites[0]
	if write.Name != "Bad checkout" || write.Description != "from console" || len(write.Turns) != 2 || write.Turns[0] != "where is my order" || write.Turns[1] != "it failed" {
		t.Fatalf("write = %#v", write)
	}
	if strings.Contains(string(write.Checkpoints), "I refunded it") || string(write.Checkpoints) != `["refunded"]` {
		t.Fatalf("checkpoints = %s", write.Checkpoints)
	}
	if string(write.InputFiles) != `[{"name":"/uploads/brief.txt","file_id":"file_in"}]` || len(write.ReferenceFiles) != 0 {
		t.Fatalf("files input=%s reference=%s", write.InputFiles, write.ReferenceFiles)
	}
	if !strings.Contains(response.Body.String(), `"version_id":"edv_test"`) || strings.Contains(response.Body.String(), "I refunded it") || strings.Contains(response.Body.String(), "file_out") {
		t.Fatalf("body = %s", response.Body.String())
	}
	store.sessions = []db.Session{{ExternalID: "sesn_plain"}}
	store.sessionEvents = []db.SessionEvent{{EventType: "user.message", Payload: json.RawMessage(`{"text":"first turn only"}`)}}
	store.resources = nil
	plain := serveEvaluation(evaluationRouter(store), http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), `{"session_id":"sesn_plain"}`, "")
	if plain.Code != http.StatusOK || store.createdWrites[0].Name != "first turn only" || len(store.createdWrites[0].InputFiles) != 0 {
		t.Fatalf("plain = %d write=%#v %s", plain.Code, store.createdWrites, plain.Body.String())
	}
}
