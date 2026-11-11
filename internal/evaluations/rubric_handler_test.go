package evaluations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const rubricGenerateBody = `{"name":"质量标准","goal":"回答准确"}`
const sampleRubric = "## 规则\n\n- 5分：正确"

func TestRubricVersionCountsCharacters(t *testing.T) {
	body := strings.Repeat("测", maxRubricLength)
	got, err := rubricVersion(rubricWriteRequest{Body: body})
	if err != nil || got.Body != body {
		t.Fatalf("body err=%v", err)
	}
	_, err = rubricVersion(rubricWriteRequest{Body: body + "测"})
	if publicMessage(err) != rubricTooLongMessage {
		t.Fatalf("message = %q", publicMessage(err))
	}
}

func TestRubricVersionRejectsLongModelByItsOwnMessage(t *testing.T) {
	model := strings.Repeat("模", maxModelLength)
	got, err := rubricVersion(rubricWriteRequest{Body: "规则", Model: model})
	if err != nil || got.Model != model {
		t.Fatalf("model err=%v", err)
	}
	_, err = rubricVersion(rubricWriteRequest{Body: "规则", Model: model + "模"})
	if publicMessage(err) != modelTooLongMessage {
		t.Fatalf("message = %q", publicMessage(err))
	}
}

func TestGenerateRubricStoresMarkdown(t *testing.T) {
	handler, store, model := newRubricGenerate(sampleRubric)
	if err := generateRubric(handler); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.system, "Markdown") || strings.Contains(model.system, "one JSON object") {
		t.Fatalf("system = %q", model.system)
	}
	if len(store.created) != 1 || store.created[0].Body != sampleRubric {
		t.Fatalf("created = %+v", store.created)
	}
}

func TestGenerateRubricRejectsJSONObject(t *testing.T) {
	handler, store, model := newRubricGenerate(`{"body":"规则"}`)
	err := generateRubric(handler)
	if publicMessage(err) != rubricFormatMessage || len(store.created) != 0 {
		t.Fatalf("err=%v created=%d", err, len(store.created))
	}
	model.text = `["规则"]`
	err = generateRubric(handler)
	if publicMessage(err) != rubricFormatMessage || len(store.created) != 0 {
		t.Fatalf("array err=%v created=%d", err, len(store.created))
	}
}

func TestGenerateRubricUnwrapsMarkdownFence(t *testing.T) {
	handler, store, _ := newRubricGenerate("```markdown\n" + sampleRubric + "\n```")
	if err := generateRubric(handler); err != nil {
		t.Fatal(err)
	}
	if len(store.created) != 1 || store.created[0].Body != sampleRubric {
		t.Fatalf("body = %q", store.created[0].Body)
	}
}

func newRubricGenerate(text string) (*Handler, *rubricMemoryStore, *scriptModel) {
	store := &rubricMemoryStore{}
	model := &scriptModel{text: text}
	return NewHandler(store, nil).WithRubricModel(model), store, model
}

func generateRubric(handler *Handler) error {
	return handler.generateRubric(httptest.NewRecorder(), rubricGenerateRequest(rubricGenerateBody))
}

func rubricGenerateRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/evaluations/rubrics/generate", strings.NewReader(body))
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"})
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", "org")
	route.URLParams.Add("workspaceId", "ws")
	return request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
}

func publicMessage(err error) string {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.PublicMessage
	}
	return ""
}

type scriptModel struct {
	system string
	text   string
}

func (m *scriptModel) Complete(_ context.Context, request CompletionRequest) (Completion, error) {
	m.system = request.System
	return Completion{Text: m.text}, nil
}

type rubricMemoryStore struct {
	Store
	created []db.EvaluationRubric
}

func (s *rubricMemoryStore) CreateEvaluationRubric(_ context.Context, rubric db.EvaluationRubric) (db.EvaluationRubric, error) {
	rubric.Version = 1
	s.created = append(s.created, rubric)
	return rubric, nil
}
