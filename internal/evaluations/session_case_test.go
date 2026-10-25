package evaluations

import (
	"context"
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

func TestCreateCaseFromSessionCopiesFileBytes(t *testing.T) {
	store := sessionCaseStore()
	copier := &fakeCaseFileCopier{errFor: map[string]error{"file_missing": errCaseFileNotFound}}
	store.resources = append(store.resources, db.SessionResource{
		SessionExternalID: "sesn_case",
		ResourceType:      "file",
		Payload:           json.RawMessage(`{"file_id":"file_missing","mount_path":"/uploads/gone.txt"}`),
	})
	handler := NewHandler(store, nil).WithCaseFileCopier(copier)
	body := `{"session_id":"sesn_case","include_output_files":false}`
	response := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), body, "")
	if response.Code != http.StatusOK || store.createCaseCalls != 1 {
		t.Fatalf("copy = %d calls=%d %s", response.Code, store.createCaseCalls, response.Body.String())
	}
	write := store.createdWrites[0]
	if string(write.InputFiles) != `[{"name":"/uploads/brief.txt","file_id":"file_copy_file_in"}]` || len(write.ReferenceFiles) != 0 {
		t.Fatalf("files input=%s reference=%s copies=%#v", write.InputFiles, write.ReferenceFiles, copier.copies)
	}
	if len(copier.copies) != 2 || copier.copies[0].FileID != "file_in" || copier.copies[1].FileID != "file_missing" {
		t.Fatalf("copies = %#v", copier.copies)
	}
	optIn := `{"session_id":"sesn_case","include_output_files":true}`
	opted := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-session"), optIn, "")
	if opted.Code != http.StatusOK || string(store.createdWrites[0].ReferenceFiles) != `[{"name":"/outputs/report.txt","file_id":"file_copy_file_out"}]` {
		t.Fatalf("opt in = %d reference=%s %s", opted.Code, store.createdWrites[0].ReferenceFiles, opted.Body.String())
	}
}

func TestCreateCasesFromSessionsReportsEachSession(t *testing.T) {
	title := "refund"
	store := &evaluationStoreStub{
		datasets: []db.EvaluationDataset{{ExternalID: "evalds_test"}},
		sessions: []db.Session{
			{ExternalID: "sesn_ok", Title: &title},
			{ExternalID: "sesn_score", Metadata: json.RawMessage(`{"evaluation_role":"agent_score"}`)},
			{ExternalID: "sesn_empty"},
		},
		sessionEvents: []db.SessionEvent{
			{SessionExternalID: "sesn_ok", EventType: "user.message", Payload: json.RawMessage(`{"text":"where is my order"}`)},
			{SessionExternalID: "sesn_empty", EventType: "agent.message", Payload: json.RawMessage(`{"text":"only the agent"}`)},
			{SessionExternalID: "sesn_score", EventType: "user.message", Payload: json.RawMessage(`{"text":"grade"}`)},
		},
	}
	router := evaluationRouter(store)
	empty := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-sessions"), `{"session_ids":[]}`, "")
	if empty.Code != http.StatusBadRequest || !strings.Contains(empty.Body.String(), sessionIDsRequiredMessage) || store.createCaseCalls != 0 {
		t.Fatalf("empty = %d calls=%d %s", empty.Code, store.createCaseCalls, empty.Body.String())
	}
	tooMany := `{"session_ids":[` + strings.TrimRight(strings.Repeat(`"sesn_ok",`, 51), ",") + `]}`
	capped := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-sessions"), tooMany, "")
	if capped.Code != http.StatusBadRequest || !strings.Contains(capped.Body.String(), sessionIDsTooManyMessage) || store.getCalls != 0 {
		t.Fatalf("cap = %d calls=%d %s", capped.Code, store.getCalls, capped.Body.String())
	}
	archivedAt := time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
	store.datasets[0].ArchivedAt = &archivedAt
	archived := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-sessions"), `{"session_ids":["sesn_ok"]}`, "")
	if archived.Code != http.StatusBadRequest || !strings.Contains(archived.Body.String(), datasetArchivedMessage) || store.getSessionCalls != 0 || store.createCaseCalls != 0 {
		t.Fatalf("archived = %d sessionCalls=%d %s", archived.Code, store.getSessionCalls, archived.Body.String())
	}
	store.datasets[0].ArchivedAt = nil
	body := `{"session_ids":["sesn_ok","sesn_missing","sesn_score","sesn_empty"],"name_prefix":"bad-","desc":"batch","include_output_files":true}`
	response := serveEvaluation(router, http.MethodPost, evaluationDatasetPath("/evalds_test/cases:from-sessions"), body, "")
	if response.Code != http.StatusOK || store.createCaseCalls != 1 || store.createdWrites[0].Name != "bad-refund" || store.createdWrites[0].Description != "batch" {
		t.Fatalf("batch = %d calls=%d write=%#v %s", response.Code, store.createCaseCalls, store.createdWrites, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"id":"`+store.createdWrites[0].ExternalID+`"`) || !strings.Contains(response.Body.String(), sessionNotFoundMessage) || !strings.Contains(response.Body.String(), scoringSessionMessage) || !strings.Contains(response.Body.String(), turnsRequiredMessage) {
		t.Fatalf("body = %s", response.Body.String())
	}
	if strings.Count(response.Body.String(), `"session_id"`) != 3 {
		t.Fatalf("failed count body = %s", response.Body.String())
	}
}

func sessionCaseStore() *evaluationStoreStub {
	title := "Bad checkout"
	return &evaluationStoreStub{
		datasets: []db.EvaluationDataset{{ExternalID: "evalds_test"}},
		sessions: []db.Session{{ExternalID: "sesn_case", Title: &title}},
		sessionEvents: []db.SessionEvent{
			{EventType: "user.message", Payload: json.RawMessage(`{"text":"where is my order"}`)},
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
		},
	}
}

type fakeCaseFileCopier struct {
	copies []sessionCaseFileRef
	errFor map[string]error
}

func (f *fakeCaseFileCopier) CopyCaseFile(_ context.Context, _ string, source sessionCaseFileRef) (sessionCaseFileRef, error) {
	f.copies = append(f.copies, source)
	if err := f.errFor[source.FileID]; err != nil {
		return sessionCaseFileRef{}, err
	}
	return sessionCaseFileRef{Name: source.Name, FileID: "file_copy_" + source.FileID}, nil
}

func (f *fakeCaseFileCopier) PutCaseFile(_ context.Context, _, name string, _ []byte) (sessionCaseFileRef, error) {
	return sessionCaseFileRef{Name: name, FileID: "file_put"}, nil
}
