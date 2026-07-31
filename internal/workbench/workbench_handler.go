package workbench

import (
	"context"
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

type workbenchPersistenceStore interface {
	GetWorkbenchPrompt(ctx context.Context, orgUUID string, promptUUID string) (*WorkbenchPromptRecord, error)
	ListWorkbenchPrompts(ctx context.Context, orgUUID string, workspaceUUID string) ([]WorkbenchPromptRecord, error)
	UpsertWorkbenchPrompt(ctx context.Context, record WorkbenchPromptRecord) (WorkbenchPromptRecord, error)
	DeleteWorkbenchPromptState(
		ctx context.Context,
		orgUUID string,
		promptUUID string,
		workspaceUUID string,
		workspaceDisplayID string,
	) error
	GetWorkbenchRevision(ctx context.Context, orgUUID string, promptUUID string, revisionUUID string) (*WorkbenchRevisionRecord, error)
	UpsertWorkbenchRevision(ctx context.Context, record WorkbenchRevisionRecord) error
	ListWorkbenchEvaluationRevisionIDs(ctx context.Context, orgUUID string) ([]string, error)
	GetWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) (*WorkbenchKVRecord, error)
	UpsertWorkbenchKV(ctx context.Context, record WorkbenchKVRecord) error
	DeleteWorkbenchKV(ctx context.Context, orgUUID string, promptUUID string, key string) error
	ListWorkbenchEvaluations(ctx context.Context, orgUUID string, revisionUUID string) ([]WorkbenchEvaluationRecord, error)
	GetWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*WorkbenchEvaluationRecord, error)
	UpsertWorkbenchEvaluation(ctx context.Context, record WorkbenchEvaluationRecord) error
	DeleteWorkbenchEvaluation(ctx context.Context, orgUUID string, evaluationUUID string) (*WorkbenchEvaluationRecord, error)
	AppendWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, values map[string]any) error
	TakeWorkbenchGeneratedTestCase(ctx context.Context, orgUUID string, requested map[string]any) (map[string]any, bool, error)
}

type workbenchWorkspaceLister interface {
	ListConsoleWorkspaces(ctx context.Context, orgUUID string, includeArchived bool) ([]ConsoleWorkspace, error)
}

type workbenchHandler struct {
	store      workbenchPersistenceStore
	workspaces workbenchWorkspaceLister
	upstream   config.AnthropicUpstreamConfig
	logger     *slog.Logger
}

func newWorkbenchHandler(store OrganizationStore, upstream config.AnthropicUpstreamConfig, logger *slog.Logger) *workbenchHandler {
	return &workbenchHandler{
		store:      workbenchPersistenceFromStore(store),
		workspaces: workbenchWorkspaceListerFromStore(store),
		upstream:   upstream,
		logger:     logging.LoggerOrDefault(logger),
	}
}

func (h *workbenchHandler) registerRoutes(r chi.Router) {
	r.Get("/models", h.handleWorkbenchModels)
	r.Get("/rate_limits_v2", h.handleWorkbenchRateLimitsV2)
	r.Get("/workspaces/{workspaceId}/rate_limits", h.handleWorkbenchWorkspaceRateLimits)
	r.Get("/workspaces/{workspaceId}/prompts", h.handleListWorkbenchWorkspacePrompts)
	r.Post("/workspaces/{workspaceId}/prompts", h.handleCreateWorkbenchPrompt)

	r.Get("/workbench/prompts", h.handleListWorkbenchPrompts)
	r.Get("/workbench/prompts/{promptUuid}", h.handleGetWorkbenchPrompt)
	r.Put("/workbench/prompts/{promptUuid}", h.handleUpdateWorkbenchPrompt)
	r.Delete("/workbench/prompts/{promptUuid}", h.handleDeleteWorkbenchPrompt)
	r.Post("/workbench/prompts/{promptUuid}/admin_delete", h.handleDeleteWorkbenchPrompt)
	r.Post("/workbench/prompts/{promptUuid}/sharing", h.handleUpdateWorkbenchPromptSharing)
	r.Get("/workbench/prompts/{promptUuid}/revisions", h.handleListWorkbenchPromptRevisions)
	r.Post("/workbench/prompts/{promptUuid}/revisions", h.handleCreateWorkbenchPromptRevision)
	r.Get("/workbench/prompts/{promptUuid}/revisions/{revisionUuid}", h.handleGetWorkbenchPromptRevision)
	r.Post("/workbench/prompts/{promptUuid}/revisions/{revisionUuid}/rename", h.handleGetWorkbenchPromptRevision)
	r.Get("/workbench/prompts/{promptUuid}/kv_store/get/{key}", h.handleWorkbenchKVGet)
	r.Post("/workbench/prompts/{promptUuid}/kv_store/set/{key}", h.handleWorkbenchKVSet)
	r.Get("/workbench/revisions/{revisionUuid}/evaluations/list", h.handleWorkbenchEvaluationsList)
	r.Post("/workbench/revisions/{revisionUuid}/evaluations/create", h.handleWorkbenchCreateEvaluation)
	r.Post("/workbench/evaluations/{evaluationUuid}/save_completion", h.handleWorkbenchOK)
	r.Post("/workbench/evaluations/{evaluationUuid}/update_variables", h.handleWorkbenchOK)
	r.Post("/workbench/evaluations/{evaluationUuid}/update_golden_answer", h.handleWorkbenchOK)
	r.Post("/workbench/evaluations/{evaluationUuid}/update_rating", h.handleWorkbenchOK)
	r.Post("/workbench/evaluations/{evaluationUuid}/delete", h.handleWorkbenchDeleteEvaluation)
	r.Delete("/workbench/evaluations/{evaluationUuid}", h.handleWorkbenchDeleteEvaluation)
	r.Post("/workbench/feedback", h.handleWorkbenchOK)

	r.Post("/workbench/completions", h.handleWorkbenchCompletions)
	r.Post("/workbench/generate_prompt", h.handleWorkbenchGeneratePrompt)
	r.Post("/workbench/generate_title", h.handleWorkbenchGenerateTitle)
	r.Post("/workbench/evaluations/generate_test_case", h.handleWorkbenchGenerateTestCase)
	r.Post("/workbench/metaprompt/generate_test_cases", h.handleWorkbenchGenerateTestCases)
	r.Post("/workbench/metaprompt/convert_prompt/{action}", h.handleWorkbenchStream(""))
}

func workbenchPersistenceFromStore(store OrganizationStore) workbenchPersistenceStore {
	persistence, _ := store.(workbenchPersistenceStore)
	return persistence
}

func workbenchWorkspaceListerFromStore(store OrganizationStore) workbenchWorkspaceLister {
	lister, _ := store.(workbenchWorkspaceLister)
	return lister
}
