package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestUpdateCaseReadsSessionAttachmentFiles(t *testing.T) {
	store := &evaluationFixture{sessionUUID: "session-uuid"}
	handler := NewHandler(store, nil)
	body := `{"query":"看这张图","source_session_id":"sesn_1","inputs":[{"type":"image","source":{"type":"file","file_id":"file_pic"}}]}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request = withEvaluationRoute(request, "evalds_1", "evalcase_1")
	response := httptest.NewRecorder()

	if err := handler.updateCase(response, request); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.attachmentSession != "session-uuid" {
		t.Fatalf("attachment session = %q", store.attachmentSession)
	}
}

func TestUpdateCaseKeepsLibraryFilesWhenSessionIsGone(t *testing.T) {
	store := &evaluationFixture{libraryMime: "image/png"}
	handler := NewHandler(store, nil)
	body := `{"query":"看这张图","source_session_id":"sesn_missing","inputs":[{"type":"image","source":{"type":"file","file_id":"file_pic"}}]}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request = withEvaluationRoute(request, "evalds_1", "evalcase_1")
	response := httptest.NewRecorder()

	if err := handler.updateCase(response, request); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.sessionLookups != 0 {
		t.Fatalf("session lookups = %d", store.sessionLookups)
	}
}

func TestUpdateCaseReportsMissingSession(t *testing.T) {
	store := &evaluationFixture{}
	handler := NewHandler(store, nil)
	body := `{"query":"看这张图","source_session_id":"sesn_missing","inputs":[{"type":"image","source":{"type":"file","file_id":"file_pic"}}]}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request = withEvaluationRoute(request, "evalds_1", "evalcase_1")

	err := handler.updateCase(httptest.NewRecorder(), request)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.NotFound || appErr.PublicMessage != "Session not found" {
		t.Fatalf("error = %#v", err)
	}
	if store.attachmentSession != "" {
		t.Fatalf("attachment session = %q", store.attachmentSession)
	}
}

func TestGenerateCasesStripsMarkdownFence(t *testing.T) {
	store := &evaluationFixture{}
	handler := NewHandler(store, nil).WithRubricModel(fixedModel{text: "```json\n[{\"query\":\"汇总销售额\"}]\n```"})
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"count":1}`))
	request = withEvaluationRoute(request, "evalds_1", "")
	response := httptest.NewRecorder()

	if err := handler.generateCases(response, request); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "汇总销售额") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestGenerateCasesReportsUnreadableModelOutput(t *testing.T) {
	handler := NewHandler(&evaluationFixture{}, nil).WithRubricModel(fixedModel{text: "not a case list"})
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"count":1}`))
	request = withEvaluationRoute(request, "evalds_1", "")

	err := handler.generateCases(httptest.NewRecorder(), request)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.Internal || appErr.PublicMessage != generateFailedMessage {
		t.Fatalf("error = %#v", err)
	}
}

func withEvaluationRoute(request *http.Request, datasetID, caseID string) *http.Request {
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", "org")
	route.URLParams.Add("workspaceId", "ws")
	route.URLParams.Add("datasetId", datasetID)
	if caseID != "" {
		route.URLParams.Add("caseId", caseID)
	}
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, route)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	return request.WithContext(auth.WithPrincipal(ctx, principal))
}

type fixedModel struct{ text string }

func (m fixedModel) Complete(context.Context, CompletionRequest) (string, error) {
	return m.text, nil
}

type evaluationFixture struct {
	sessionUUID       string
	attachmentSession string
	sessionLookups    int
	libraryMime       string
}

func (evaluationFixture) CreateEvaluationDataset(context.Context, db.EvaluationDataset, []db.EvaluationCaseWrite) (db.EvaluationDataset, error) {
	return db.EvaluationDataset{}, nil
}

func (evaluationFixture) ListEvaluationDatasets(context.Context, string, string, int, int) ([]db.EvaluationDataset, error) {
	return nil, nil
}

func (evaluationFixture) GetEvaluationDataset(context.Context, string, string, string) (db.EvaluationDataset, error) {
	return db.EvaluationDataset{}, nil
}

func (evaluationFixture) DeleteEvaluationDataset(context.Context, string, string, string, time.Time) error {
	return nil
}

func (evaluationFixture) ListEvaluationCases(context.Context, string, string, string) ([]db.EvaluationCase, error) {
	return nil, nil
}

func (evaluationFixture) GetEvaluationCaseTrace(context.Context, string, string, string, string) (json.RawMessage, error) {
	return nil, nil
}

func (evaluationFixture) CreateEvaluationCases(context.Context, string, string, string, []db.EvaluationCaseWrite, time.Time) ([]db.EvaluationCase, error) {
	return []db.EvaluationCase{{ExternalID: "evalcase_1", Query: "汇总销售额"}}, nil
}

func (evaluationFixture) UpdateEvaluationCase(context.Context, string, string, string, db.EvaluationCaseWrite, time.Time) (db.EvaluationCase, error) {
	return db.EvaluationCase{ExternalID: "evalcase_1", Query: "看这张图"}, nil
}

func (evaluationFixture) DeleteEvaluationCase(context.Context, string, string, string, string, time.Time) error {
	return nil
}

func (evaluationFixture) CreateEvaluationRubric(context.Context, db.EvaluationRubric) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{}, nil
}

func (evaluationFixture) AppendEvaluationRubricVersion(context.Context, string, string, string, db.EvaluationRubric, time.Time) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{}, nil
}

func (evaluationFixture) ListEvaluationRubrics(context.Context, string, string, int, int) ([]db.EvaluationRubric, error) {
	return nil, nil
}

func (evaluationFixture) GetEvaluationRubric(context.Context, string, string, string) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{}, nil
}

func (evaluationFixture) ListEvaluationRubricVersions(context.Context, string, string, string) ([]db.EvaluationRubric, error) {
	return nil, nil
}

func (evaluationFixture) DeleteEvaluationRubric(context.Context, string, string, string, time.Time) error {
	return nil
}

func (evaluationFixture) CreateEvaluationTask(context.Context, db.EvaluationTask, []db.EvaluationTaskItem) (db.EvaluationTaskDetail, error) {
	return db.EvaluationTaskDetail{}, nil
}

func (evaluationFixture) ListEvaluationTasks(context.Context, string, string, int, int) ([]db.EvaluationTask, error) {
	return nil, nil
}

func (evaluationFixture) ListEvaluationTaskChildren(context.Context, string, string, []string) ([]db.EvaluationTaskItem, []db.EvaluationTaskScore, error) {
	return nil, nil, nil
}

func (evaluationFixture) GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error) {
	return db.EvaluationTaskDetail{}, nil
}

func (evaluationFixture) ListRunningEvaluationItemsBySession(context.Context, string, string, string) ([]db.EvaluationTaskItem, error) {
	return nil, nil
}

func (evaluationFixture) MarkEvaluationItemRunning(context.Context, string, string, string, string, time.Time) (bool, error) {
	return false, nil
}

func (evaluationFixture) FinishRunningEvaluationItem(context.Context, string, string, string, string, string, time.Time) (bool, error) {
	return false, nil
}

func (evaluationFixture) ResetFailedEvaluationItems(context.Context, string, string, string, int, time.Time) (int64, error) {
	return 0, nil
}

func (evaluationFixture) CancelPendingEvaluationItems(context.Context, string, string, string, time.Time) error {
	return nil
}

func (evaluationFixture) UpdateEvaluationTaskStatus(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}

func (evaluationFixture) CreateEvaluationTaskScore(context.Context, db.EvaluationTaskScore) (db.EvaluationTaskScore, error) {
	return db.EvaluationTaskScore{}, nil
}

func (evaluationFixture) GetEnvironment(context.Context, string, string) (db.Environment, error) {
	return db.Environment{}, nil
}

func (evaluationFixture) GetAgent(context.Context, string, string) (db.Agent, error) {
	return db.Agent{}, nil
}

func (evaluationFixture) GetAgentVersion(context.Context, string, string, int) (db.Agent, error) {
	return db.Agent{}, nil
}

func (f *evaluationFixture) GetSession(context.Context, string, string) (db.Session, bool, error) {
	f.sessionLookups++
	return db.Session{UUID: f.sessionUUID}, f.sessionUUID != "", nil
}

func (evaluationFixture) ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	return nil, false, nil
}

func (f *evaluationFixture) GetFile(context.Context, string, string) (db.FileRecord, error) {
	if f.libraryMime == "" {
		return db.FileRecord{}, db.ErrNotFound
	}
	return db.FileRecord{MimeType: f.libraryMime}, nil
}

func (f *evaluationFixture) GetSessionAttachmentFile(_ context.Context, _, sessionUUID, _ string) (db.FileRecord, error) {
	f.attachmentSession = sessionUUID
	return db.FileRecord{MimeType: "image/png"}, nil
}
