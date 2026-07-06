package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	workbenchDefaultPromptID   = "52aa673d-8d88-408d-849d-e4c2a8e33144"
	workbenchDefaultRevisionID = "04977f32-f204-443c-8d3e-ed5aac2673aa"
	workbenchDefaultCreatedAt  = "2026-06-12T02:10:24.382428Z"
	workbenchDefaultModel      = "claude-opus-4-8"
)

var platformClaudeLegacyRateLimitModelGroups = []string{
	"claude_fable_5",
	"claude_haiku_4",
	"claude_opus_4_5",
	"claude_sonnet_4",
}

const (
	platformClaudeFastInputTokensPerMinute  = 10000
	platformClaudeFastOutputTokensPerMinute = 4000
)

var workbenchLocalRevisions sync.Map
var workbenchLocalLatestRevisionIDs sync.Map
var workbenchLocalKV sync.Map
var workbenchLocalPromptNames sync.Map
var workbenchLocalPromptSharing sync.Map
var workbenchLocalDeletedPrompts sync.Map
var workbenchLocalEvaluations sync.Map
var workbenchLocalEvaluationMu sync.Mutex
var workbenchLocalGeneratedTestCases sync.Map
var workbenchLocalGeneratedTestCaseMu sync.Mutex

type workbenchKVEntry struct {
	Value   string
	Version any
}

type workbenchPersistenceContextKey struct{}

type workbenchPersistenceStore interface {
	GetWorkbenchPrompt(ctx context.Context, orgUUID string, promptUUID string) (*WorkbenchPromptRecord, error)
	ListWorkbenchPrompts(ctx context.Context, orgUUID string, workspaceID string) ([]WorkbenchPromptRecord, error)
	UpsertWorkbenchPrompt(ctx context.Context, record WorkbenchPromptRecord) (WorkbenchPromptRecord, error)
	DeleteWorkbenchPromptState(ctx context.Context, orgUUID string, promptUUID string) error
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

func registerOrgWorkbenchRoutes(r chi.Router, store OrganizationStore) {
	workbenchStore := workbenchPersistenceFromStore(store)
	h := func(handler http.HandlerFunc) http.HandlerFunc {
		return withWorkbenchPersistence(workbenchStore, handler)
	}
	r.Get("/models", h(handleWorkbenchModels))
	r.Get("/rate_limits_v2", h(handleWorkbenchRateLimitsV2))
	r.Get("/workspaces/{workspaceId}/rate_limits", h(handleWorkbenchWorkspaceRateLimits))
	r.Get("/workspaces/{workspaceId}/prompts", h(handleListWorkbenchWorkspacePrompts))
	r.Post("/workspaces/{workspaceId}/prompts", h(handleCreateWorkbenchPrompt))

	r.Get("/workbench/prompts", h(handleListWorkbenchPrompts))
	r.Get("/workbench/prompts/{promptUuid}", h(handleGetWorkbenchPrompt))
	r.Put("/workbench/prompts/{promptUuid}", h(handleUpdateWorkbenchPrompt))
	r.Delete("/workbench/prompts/{promptUuid}", h(handleDeleteWorkbenchPrompt))
	r.Post("/workbench/prompts/{promptUuid}/admin_delete", h(handleDeleteWorkbenchPrompt))
	r.Post("/workbench/prompts/{promptUuid}/sharing", h(handleUpdateWorkbenchPromptSharing))
	r.Get("/workbench/prompts/{promptUuid}/revisions", h(handleListWorkbenchPromptRevisions))
	r.Post("/workbench/prompts/{promptUuid}/revisions", h(handleCreateWorkbenchPromptRevision))
	r.Get("/workbench/prompts/{promptUuid}/revisions/{revisionUuid}", h(handleGetWorkbenchPromptRevision))
	r.Post("/workbench/prompts/{promptUuid}/revisions/{revisionUuid}/rename", h(handleGetWorkbenchPromptRevision))
	r.Get("/workbench/prompts/{promptUuid}/kv_store/get/{key}", h(handleWorkbenchKVGet))
	r.Post("/workbench/prompts/{promptUuid}/kv_store/set/{key}", h(handleWorkbenchKVSet))
	r.Get("/workbench/revisions/{revisionUuid}/evaluations/list", h(handleWorkbenchEvaluationsList))
	r.Post("/workbench/revisions/{revisionUuid}/evaluations/create", h(handleWorkbenchCreateEvaluation))
	r.Post("/workbench/evaluations/{evaluationUuid}/save_completion", h(handleWorkbenchOK))
	r.Post("/workbench/evaluations/{evaluationUuid}/update_variables", h(handleWorkbenchOK))
	r.Post("/workbench/evaluations/{evaluationUuid}/update_golden_answer", h(handleWorkbenchOK))
	r.Post("/workbench/evaluations/{evaluationUuid}/update_rating", h(handleWorkbenchOK))
	r.Post("/workbench/evaluations/{evaluationUuid}/delete", h(handleWorkbenchDeleteEvaluation))
	r.Delete("/workbench/evaluations/{evaluationUuid}", h(handleWorkbenchDeleteEvaluation))
	r.Post("/workbench/feedback", h(handleWorkbenchOK))

	r.Post("/workbench/completions", h(handleWorkbenchCompletions))
	r.Post("/workbench/generate_prompt", h(handleWorkbenchGeneratePrompt))
	r.Post("/workbench/generate_title", h(handleWorkbenchGenerateTitle))
	r.Post("/workbench/evaluations/generate_test_case", h(handleWorkbenchGenerateTestCase))
	r.Post("/workbench/metaprompt/generate_test_cases", h(handleWorkbenchGenerateTestCases))
	r.Post("/workbench/metaprompt/convert_prompt/{action}", h(handleWorkbenchStream("")))
}

func workbenchPersistenceFromStore(store OrganizationStore) workbenchPersistenceStore {
	persistence, _ := store.(workbenchPersistenceStore)
	return persistence
}

func withWorkbenchPersistence(store workbenchPersistenceStore, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store != nil {
			r = r.WithContext(context.WithValue(r.Context(), workbenchPersistenceContextKey{}, store))
		}
		handler(w, r)
	}
}

func workbenchPersistenceFromRequest(r *http.Request) workbenchPersistenceStore {
	store, _ := r.Context().Value(workbenchPersistenceContextKey{}).(workbenchPersistenceStore)
	return store
}

func workbenchWritePersistenceError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error":   "workbench_persistence_error",
		"message": err.Error(),
	})
	return true
}

func handleListWorkbenchPrompts(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	workspaceID := strings.TrimSpace(r.Header.Get("X-Workspace-ID"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	}
	if workspaceID == "" {
		workspaceID = "default"
	}
	prompts := make([]any, 0, 4)
	seen := map[string]bool{}
	deleted, err := workbenchPromptDeleted(r, workbenchDefaultPromptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if !deleted {
		prompts = append(prompts, workbenchPromptSummary(r, workbenchDefaultPromptID, "default", ""))
		seen[workbenchDefaultPromptID] = true
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		records, err := store.ListWorkbenchPrompts(r.Context(), workbenchOrgUUID(r), workspaceID)
		if workbenchWritePersistenceError(w, err) {
			return
		}
		for _, record := range records {
			promptID := strings.TrimSpace(record.PromptUUID)
			if promptID == "" || seen[promptID] || record.DeletedAt != nil {
				continue
			}
			prompts = append(prompts, workbenchPromptSummary(r, promptID, record.WorkspaceID, record.Name))
			seen[promptID] = true
		}
	}
	writeJSON(w, http.StatusOK, prompts)
}

func handleListWorkbenchWorkspacePrompts(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if workspaceID == "" {
		workspaceID = "default"
	}
	prompts := make([]any, 0, 4)
	seen := map[string]bool{}
	deleted, err := workbenchPromptDeleted(r, workbenchDefaultPromptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if !deleted {
		prompt := workbenchPromptSummary(r, workbenchDefaultPromptID, workspaceID, "")
		if promptWorkspaceID, _ := prompt["workspace_id"].(string); strings.TrimSpace(promptWorkspaceID) == workspaceID {
			prompts = append(prompts, prompt)
			seen[workbenchDefaultPromptID] = true
		}
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		records, err := store.ListWorkbenchPrompts(r.Context(), workbenchOrgUUID(r), workspaceID)
		if workbenchWritePersistenceError(w, err) {
			return
		}
		for _, record := range records {
			promptID := strings.TrimSpace(record.PromptUUID)
			if promptID == "" || seen[promptID] || record.DeletedAt != nil {
				continue
			}
			prompts = append(prompts, workbenchPromptSummary(r, promptID, record.WorkspaceID, record.Name))
			seen[promptID] = true
		}
	}
	writeJSON(w, http.StatusOK, prompts)
}

func handleCreateWorkbenchPrompt(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, _ := readJSONObject(r)
	if body == nil {
		body = map[string]any{}
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if workspaceID == "" {
		workspaceID = "default"
	}
	promptID := workbenchDefaultPromptID
	if err := workbenchUndeletePrompt(r, promptID, workspaceID); workbenchWritePersistenceError(w, err) {
		return
	}
	name, hasName := body["name"].(string)
	if hasName {
		if err := workbenchStorePromptName(r, promptID, name); workbenchWritePersistenceError(w, err) {
			return
		}
	}
	if revisionBody, ok := body["latest_revision"].(map[string]any); ok {
		revision := workbenchRevisionFromBody(r, revisionBody, "workbench-revision-"+uuid.NewString(), true, false)
		if err := workbenchStoreRevision(r, promptID, revision); workbenchWritePersistenceError(w, err) {
			return
		}
	}
	writeJSON(w, http.StatusOK, workbenchPromptDetail(r, promptID, workspaceID, name))
}

func handleGetWorkbenchPrompt(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, workbenchPromptDetail(r, promptID, "default", ""))
}

func handleUpdateWorkbenchPrompt(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, _ := readJSONObject(r)
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	name, _ := body["name"].(string)
	if _, ok := body["name"]; ok {
		if err := workbenchStorePromptName(r, promptID, name); workbenchWritePersistenceError(w, err) {
			return
		}
	}
	writeJSON(w, http.StatusOK, workbenchPromptDetail(r, promptID, "default", name))
}

func handleUpdateWorkbenchPromptSharing(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	if err := workbenchStorePromptSharing(r, promptID, true); workbenchWritePersistenceError(w, err) {
		return
	}
	prompt := workbenchPromptDetail(r, promptID, "default", "")
	prompt["is_shared_with_workspace"] = true
	writeJSON(w, http.StatusOK, prompt)
}

func handleListWorkbenchPromptRevisions(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	compact := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("compact")), "true")
	includeMessages := !compact
	revisions := []any{}
	hasLatest := false
	seenRevisions := map[string]bool{}
	appendRevision := func(revision map[string]any, revisionID string) {
		if seenRevisions[revisionID] {
			return
		}
		if compact {
			revision = workbenchCompactRevision(revision)
		}
		revisions = append(revisions, revision)
		seenRevisions[revisionID] = true
	}
	if revision, revisionID, ok := workbenchStoredLatestRevision(r, promptID, includeMessages, true); ok {
		appendRevision(revision, revisionID)
		hasLatest = true
	}
	for _, revisionID := range workbenchEvaluationRevisionIDs(r) {
		if seenRevisions[revisionID] {
			continue
		}
		revision, ok := workbenchRevisionFromEvaluations(r, revisionID, includeMessages, true)
		if !ok {
			continue
		}
		if hasLatest {
			revision["is_latest"] = false
		}
		appendRevision(revision, revisionID)
		hasLatest = true
	}
	defaultRevision := workbenchRevision(r, workbenchRevisionIDFromRequest(r), includeMessages, true)
	if hasLatest {
		defaultRevision["is_latest"] = false
	}
	appendRevision(defaultRevision, workbenchString(defaultRevision["id"]))
	writeJSON(w, http.StatusOK, revisions)
}

func handleCreateWorkbenchPromptRevision(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, err := readJSONObject(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body must be a JSON object"})
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	revision := workbenchRevisionFromBody(r, body, "workbench-revision-"+uuid.NewString(), true, false)
	if err := workbenchStoreRevision(r, promptID, revision); workbenchWritePersistenceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, revision)
}

func handleGetWorkbenchPromptRevision(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	revisionID := workbenchRevisionIDFromRequest(r)
	if revision, ok := workbenchStoredRevision(r, promptID, revisionID, true, false); ok {
		writeJSON(w, http.StatusOK, revision)
		return
	}
	if revision, ok := workbenchRevisionFromEvaluations(r, revisionID, true, false); ok {
		writeJSON(w, http.StatusOK, revision)
		return
	}
	writeJSON(w, http.StatusOK, workbenchRevision(r, revisionID, true, false))
}

func handleWorkbenchKVGet(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	key := chi.URLParam(r, "key")
	if entry, ok := workbenchStoredKV(r, promptID, key); ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"value":   entry.Value,
			"version": entry.Version,
		})
		return
	}
	switch key {
	case "draft_revision":
		writeJSON(w, http.StatusOK, map[string]any{"success": false})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"success": false})
	}
}

func handleWorkbenchKVSet(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, err := readJSONObject(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "request body must be a JSON object"})
		return
	}
	promptID := workbenchPromptIDFromRequest(r)
	deleted, err := workbenchPromptDeleted(r, promptID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if deleted {
		writeWorkbenchPromptNotFound(w)
		return
	}
	key := chi.URLParam(r, "key")
	value, ok := workbenchKVValueFromBody(body)
	if key == "draft_revision" && (!ok || workbenchDraftRevisionShouldClear(value)) {
		if err := workbenchDeleteKV(r, promptID, key); workbenchWritePersistenceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "version": nil})
		return
	}
	if key == "draft_revision" && ok && !workbenchDraftRevisionHasContent(value) {
		currentDraft := workbenchPromptDraftRevisionString(r, promptID)
		if workbenchDraftRevisionHasContent(currentDraft) {
			version := any(nil)
			if entry, ok := workbenchStoredKV(r, promptID, key); ok {
				version = entry.Version
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "version": version})
			return
		}
		if err := workbenchDeleteKV(r, promptID, key); workbenchWritePersistenceError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "version": nil})
		return
	}
	version, ok := body["version"]
	if !ok {
		version = formatJSISOString(time.Now())
	}
	if key == "draft_revision" {
		value = workbenchNormalizeDraftRevisionValue(value)
	}
	if err := workbenchStoreKV(r, promptID, key, workbenchKVEntry{Value: value, Version: version}); workbenchWritePersistenceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "version": version})
}

func handleWorkbenchEvaluationsList(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, workbenchStoredEvaluations(r, workbenchRevisionIDFromRequest(r)))
}

func handleWorkbenchCreateEvaluation(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, _ := readJSONObject(r)
	evaluation := workbenchEvaluationFromBody(r, body, workbenchRevisionIDFromRequest(r))
	if err := workbenchStoreEvaluation(r, evaluation); workbenchWritePersistenceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, evaluation)
}

func handleWorkbenchOK(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, _ := readJSONObject(r)
	if evaluationID := strings.TrimSpace(chi.URLParam(r, "evaluationUuid")); evaluationID != "" {
		evaluation, ok, err := workbenchUpdateEvaluation(r, evaluationID, body)
		if workbenchWritePersistenceError(w, err) {
			return
		}
		if ok {
			writeJSON(w, http.StatusOK, evaluation)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func handleWorkbenchDeleteEvaluation(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	evaluationID := strings.TrimSpace(chi.URLParam(r, "evaluationUuid"))
	deleted, ok, err := workbenchDeleteEvaluation(r, evaluationID)
	if workbenchWritePersistenceError(w, err) {
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, deleted)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func handleDeleteWorkbenchPrompt(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	if err := workbenchDeletePrompt(r, workbenchPromptIDFromRequest(r)); workbenchWritePersistenceError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func handleWorkbenchModels(w http.ResponseWriter, r *http.Request) {
	if !visibleOrgUUIDOrPlatformClaudeMirror(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"default_prompt_settings": map[string]any{
			"model_name":           workbenchDefaultModel,
			"system_prompt":        "",
			"temperature":          1,
			"max_tokens_to_sample": 20000,
		},
		"models": []any{
			workbenchModel("claude-fable-5", "Claude Fable 5", "claude_fable_5", 1000000, 128000, true, true, true),
			workbenchModel("claude-opus-4-8", "Claude Opus Active", "claude_opus_4_5", 1000000, 128000, true, true, true),
			workbenchModel("claude-sonnet-4-6", "Claude Sonnet Active", "claude_sonnet_4", 1000000, 64000, true, true, true),
			workbenchModel("claude-haiku-4-5-20251001", "Claude Haiku 4.5", "claude_haiku_4", 200000, 64000, true, false, false),
		},
	})
}

func handleRateLimits(w http.ResponseWriter, _ *http.Request) {
	limiters := make([]map[string]any, 0, 14)
	for _, modelGroup := range platformClaudeLegacyRateLimitModelGroups {
		for _, limit := range platformClaudeLegacyRateLimitsForModelGroup(modelGroup) {
			limiters = append(limiters, map[string]any{
				"limiter":     limit["type"],
				"value":       limit["value"],
				"source":      "default",
				"model_group": modelGroup,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rate_limit_tier":              "auto_api_evaluation",
		"tier_model_rate_limiters":     limiters,
		"custom_default_rate_limiters": nil,
		"custom_model_rate_limiters":   nil,
		"spend_threshold":              50000,
		"effective_rate_limiters":      nil,
	})
}

func handleWorkbenchRateLimitsV2(w http.ResponseWriter, r *http.Request) {
	if !visibleOrgUUIDOrPlatformClaudeMirror(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rate_limits":               platformClaudeRateLimitsV2(),
		"model_group_display_names": platformClaudeModelGroupDisplayNames(),
	})
}

func handleWorkbenchWorkspaceRateLimits(w http.ResponseWriter, r *http.Request) {
	if !visibleOrgUUIDOrPlatformClaudeMirror(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rate_limits": map[string]any{}})
}

func visibleOrgUUIDOrPlatformClaudeMirror(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := platformClaudeMirrorOrgUUID(r); ok {
		return true
	}
	_, ok := visibleOrgUUID(w, r)
	return ok
}

func visibleWorkbenchOrg(w http.ResponseWriter, r *http.Request) bool {
	return visibleOrgUUIDOrPlatformClaudeMirror(w, r)
}

func platformClaudeMirrorOrgUUID(r *http.Request) (string, bool) {
	orgUUID := strings.TrimSpace(chi.URLParam(r, "orgUuid"))
	principal, ok := auth.PrincipalFromContext(r.Context())
	if orgUUID == "" || !ok || !principalCanSeeOrg(principal, orgUUID) || !isPlatformClaudeHost(r.Host) {
		return "", false
	}
	return orgUUID, true
}

func platformClaudeModelGroupDisplayNames() map[string]string {
	return map[string]string{
		"claude_fable_5":               "Claude Fable 5",
		"claude_haiku_4":               "Claude Haiku 4.5",
		"claude_sonnet_4":              "Claude Sonnet Active",
		"claude_opus_4_5":              "Claude Opus Active",
		"claude_batch":                 "Message Batches API",
		"messages_api_web_search_tool": "Web search tool",
	}
}

func platformClaudeRateLimitsV2() map[string]any {
	return map[string]any{
		"claude_haiku_4":  workbenchStandardRateLimits(),
		"claude_sonnet_4": workbenchStandardRateLimits(),
		"claude_opus_4_5": append(workbenchStandardRateLimits(),
			platformClaudeRateLimit("fast_itpmca", platformClaudeFastInputTokensPerMinute),
			platformClaudeRateLimit("fast_otpm", platformClaudeFastOutputTokensPerMinute),
		),
		"claude_fable_5": workbenchStandardRateLimits(),
		"claude_batch": []any{
			platformClaudeRateLimit("enqueued_batch_requests", 50000),
			platformClaudeRateLimit("requests_per_minute", 5),
		},
		"messages_api_web_search_tool": []any{
			platformClaudeRateLimit("tool_uses_per_second", 30),
		},
	}
}

func platformClaudeLegacyRateLimitsForModelGroup(modelGroup string) []map[string]any {
	if modelGroup == "claude_opus_4_5" {
		return append([]map[string]any{
			platformClaudeRateLimit("fast_itpmca", platformClaudeFastInputTokensPerMinute),
			platformClaudeRateLimit("fast_otpm", platformClaudeFastOutputTokensPerMinute),
		}, workbenchStandardRateLimitMaps()...)
	}
	return workbenchStandardRateLimitMaps()
}

func handleWorkbenchStream(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := visibleOrgUUID(w, r); !ok {
			return
		}
		workbenchWriteCompletionStream(w, text)
	}
}

func handleWorkbenchGenerateTestCase(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, _ := readJSONObject(r)
	if text, generatedValues, ok := workbenchGenerateTestCaseFromAnthropic(r, body); ok {
		if err := workbenchStoreGeneratedTestCase(r, generatedValues); workbenchWritePersistenceError(w, err) {
			return
		}
		workbenchWriteCompletionStream(w, text)
		return
	}
	generatedValues := workbenchGeneratedVariableValues(body, 1)
	if err := workbenchStoreGeneratedTestCase(r, generatedValues); workbenchWritePersistenceError(w, err) {
		return
	}
	workbenchWriteCompletionStream(w, workbenchGeneratedTestCaseTextFromValues(generatedValues))
}

func handleWorkbenchGenerateTestCases(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	body, _ := readJSONObject(r)
	count := workbenchTestCaseCount(body)
	if generatedCases, ok := workbenchGenerateTestCasesFromAnthropic(r, body, count); ok {
		workbenchWriteGeneratedTestCasesStream(w, generatedCases)
		return
	}
	generatedCases := make([]map[string]any, 0, count)
	for idx := 1; idx <= count; idx++ {
		generatedCases = append(generatedCases, workbenchGeneratedVariableValues(body, idx))
	}
	workbenchWriteGeneratedTestCasesStream(w, generatedCases)
}

func workbenchWriteGeneratedTestCasesStream(w http.ResponseWriter, generatedCases []map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	for _, generatedValues := range generatedCases {
		workbenchWriteSSE(w, "test_case", map[string]any{
			"variable_values": generatedValues,
		})
	}
}

func workbenchGenerateTestCaseFromAnthropic(r *http.Request, body map[string]any) (string, map[string]any, bool) {
	variableNames := workbenchVariableNamesFromPayload(body)
	if len(variableNames) == 0 {
		return "", nil, false
	}
	text, _, _, ok := workbenchAnthropicTextFromBody(r, workbenchGenerateTestCaseAnthropicBody(body, variableNames))
	if !ok {
		return "", nil, false
	}
	values := workbenchTaggedVariableValues(text, variableNames)
	if !workbenchGeneratedValuesComplete(values, variableNames) {
		return "", nil, false
	}
	planning, _ := workbenchTaggedValue(text, "planning")
	return workbenchGeneratedTestCaseTextFromValuesWithPlanning(values, planning), values, true
}

func workbenchGenerateTestCasesFromAnthropic(r *http.Request, body map[string]any, count int) ([]map[string]any, bool) {
	variableNames := workbenchVariableNamesFromPayload(body)
	if len(variableNames) == 0 {
		return nil, false
	}
	text, _, _, ok := workbenchAnthropicTextFromBody(r, workbenchGenerateTestCasesAnthropicBody(body, variableNames, count))
	if !ok {
		return nil, false
	}
	generatedCases := workbenchGeneratedTestCasesFromText(text, variableNames, count)
	if len(generatedCases) == 0 {
		return nil, false
	}
	for len(generatedCases) < count {
		generatedCases = append(generatedCases, workbenchGeneratedVariableValues(body, len(generatedCases)+1))
	}
	if len(generatedCases) > count {
		generatedCases = generatedCases[:count]
	}
	return generatedCases, true
}

func workbenchAnthropicTextFromBody(r *http.Request, upstreamBody map[string]any) (string, int, int, bool) {
	token := proxyMessagesAnthropicToken()
	if token == "" {
		return "", 0, 0, false
	}
	endpoint, err := anthropicMessagesEndpoint()
	if err != nil {
		return "", 0, 0, false
	}
	body, err := json.Marshal(upstreamBody)
	if err != nil {
		return "", 0, 0, false
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, false
	}
	upstreamReq.Header.Set("Accept", "application/json")
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("X-API-Key", token)
	upstreamReq.Header.Set("Anthropic-Version", anthropicAPIVersion)

	upstreamRes, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		return "", 0, 0, false
	}
	defer upstreamRes.Body.Close()
	if upstreamRes.StatusCode < 200 || upstreamRes.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, upstreamRes.Body)
		return "", 0, 0, false
	}

	var upstream workbenchGenerateTitleResponse
	if err := json.NewDecoder(upstreamRes.Body).Decode(&upstream); err != nil {
		return "", 0, 0, false
	}
	var text strings.Builder
	for _, block := range upstream.Content {
		if block.Type != "text" || strings.TrimSpace(block.Text) == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(block.Text)
	}
	return strings.TrimSpace(text.String()), upstream.Usage.InputTokens, upstream.Usage.OutputTokens, text.Len() > 0
}

func workbenchGenerateTestCaseAnthropicBody(body map[string]any, variableNames []string) map[string]any {
	return map[string]any{
		"model":       workbenchGenerateTestCaseModel(body),
		"max_tokens":  1600,
		"temperature": 0.7,
		"stream":      false,
		"system":      workbenchGenerateTestCaseSystemPrompt(),
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": workbenchGenerateTestCasePrompt(body, variableNames),
					},
				},
			},
		},
	}
}

func workbenchGenerateTestCasesAnthropicBody(body map[string]any, variableNames []string, count int) map[string]any {
	maxTokens := 700 + count*350
	if maxTokens > 4096 {
		maxTokens = 4096
	}
	return map[string]any{
		"model":       workbenchGenerateTestCaseModel(body),
		"max_tokens":  maxTokens,
		"temperature": 0.8,
		"stream":      false,
		"system":      workbenchGenerateTestCasesSystemPrompt(),
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": workbenchGenerateTestCasesPrompt(body, variableNames, count),
					},
				},
			},
		},
	}
}

func workbenchGenerateTestCaseModel(body map[string]any) string {
	return chatCompletionModel(firstNonEmpty(
		strings.TrimSpace(os.Getenv("WORKBENCH_GENERATE_TEST_CASE_MODEL")),
		chatNormalizeString(body["model_name"]),
		chatNormalizeString(body["model"]),
		strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")),
		miscDefaultChatModel,
	))
}

func workbenchGenerateTestCaseSystemPrompt() string {
	return strings.Join([]string{
		"You generate realistic Claude Workbench evaluation test cases.",
		"Return only XML tags, with no markdown, no code fence, and no extra prose.",
		"Include one short <planning> tag, followed by exactly one tag for each requested variable name.",
		"Use the variable names exactly as provided. Do not invent, rename, or omit variables.",
		"Variable values must be concrete, diverse, and suitable for evaluating the prompt. Avoid placeholders such as Generated, example, lorem ipsum, or test data.",
		"Do not put XML tags inside variable values.",
	}, "\n")
}

func workbenchGenerateTestCasesSystemPrompt() string {
	return strings.Join([]string{
		"You generate realistic Claude Workbench evaluation test cases.",
		"Return only valid JSON, with no markdown, no code fence, and no extra prose.",
		"Every test case must include variable_values with every requested variable name exactly as provided.",
		"Variable values must be concrete, diverse, and suitable for evaluating the prompt. Avoid placeholders such as Generated, example, lorem ipsum, or test data.",
	}, "\n")
}

func workbenchGenerateTestCasePrompt(body map[string]any, variableNames []string) string {
	var b strings.Builder
	b.WriteString("Generate one realistic evaluation test case for this Claude Workbench prompt.\n")
	b.WriteString("Variables to fill: ")
	b.WriteString(strings.Join(variableNames, ", "))
	b.WriteString("\n\nReturn exactly this XML shape:\n")
	b.WriteString("<planning>one brief sentence about the scenario</planning>\n")
	for _, name := range variableNames {
		b.WriteString("<")
		b.WriteString(name)
		b.WriteString(">realistic value</")
		b.WriteString(name)
		b.WriteString(">\n")
	}
	b.WriteString("\nPrompt context JSON:\n")
	b.WriteString(workbenchFormatJSONForPrompt(workbenchGenerateTestCaseContext(body)))
	return b.String()
}

func workbenchGenerateTestCasesPrompt(body map[string]any, variableNames []string, count int) string {
	var b strings.Builder
	b.WriteString("Generate ")
	b.WriteString(strconv.Itoa(count))
	b.WriteString(" diverse realistic evaluation test cases for this Claude Workbench prompt.\n")
	b.WriteString("Variables to fill in every test case: ")
	b.WriteString(strings.Join(variableNames, ", "))
	b.WriteString("\n\nReturn JSON in this exact shape:\n")
	b.WriteString(`{"test_cases":[{"variable_values":{`)
	for idx, name := range variableNames {
		if idx > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.Quote(name))
		b.WriteString(`:"realistic value"`)
	}
	b.WriteString(`}}]}`)
	b.WriteString("\n\nPrompt context JSON:\n")
	b.WriteString(workbenchFormatJSONForPrompt(workbenchGenerateTestCaseContext(body)))
	return b.String()
}

func workbenchGenerateTestCaseContext(body map[string]any) map[string]any {
	context := map[string]any{}
	for _, key := range []string{"prompt", "system_prompt", "messages", "custom_chain_of_thought", "existing_examples", "examples"} {
		if value, ok := body[key]; ok {
			context[key] = chatClone(value)
		}
	}
	return context
}

func workbenchFormatJSONForPrompt(value any) string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func workbenchTaggedVariableValues(text string, variableNames []string) map[string]any {
	values := map[string]any{}
	for _, name := range variableNames {
		if value, ok := workbenchTaggedValue(text, name); ok {
			values[name] = value
		}
	}
	return values
}

func workbenchTaggedValue(text string, tagName string) (string, bool) {
	text = workbenchStripCodeFence(text)
	tagName = strings.TrimSpace(tagName)
	if tagName == "" {
		return "", false
	}
	openTag := "<" + tagName + ">"
	closeTag := "</" + tagName + ">"
	start := strings.Index(text, openTag)
	if start < 0 {
		return "", false
	}
	start += len(openTag)
	end := strings.Index(text[start:], closeTag)
	if end < 0 {
		return "", false
	}
	value := strings.TrimSpace(text[start : start+end])
	if value == "" {
		return "", false
	}
	return value, true
}

func workbenchGeneratedValuesComplete(values map[string]any, variableNames []string) bool {
	if len(values) != len(variableNames) || len(variableNames) == 0 {
		return false
	}
	for _, name := range variableNames {
		value, ok := values[name]
		if !ok || strings.TrimSpace(workbenchScalarString(value)) == "" {
			return false
		}
	}
	return true
}

func workbenchGeneratedTestCaseTextFromValuesWithPlanning(values map[string]any, planning string) string {
	planning = strings.TrimSpace(planning)
	if planning == "" {
		planning = "Generated realistic Workbench test case."
	}
	var text strings.Builder
	text.WriteString("<planning>")
	text.WriteString(planning)
	text.WriteString("</planning>\n")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		text.WriteString("<")
		text.WriteString(name)
		text.WriteString(">")
		text.WriteString(strings.TrimSpace(workbenchScalarString(values[name])))
		text.WriteString("</")
		text.WriteString(name)
		text.WriteString(">\n")
	}
	return text.String()
}

func workbenchGeneratedTestCasesFromText(text string, variableNames []string, count int) []map[string]any {
	payload := workbenchExtractJSONPayload(text)
	if payload == "" {
		return nil
	}
	generatedCases := []map[string]any{}
	var wrapped struct {
		TestCases []struct {
			VariableValues map[string]any `json:"variable_values"`
		} `json:"test_cases"`
	}
	if err := json.Unmarshal([]byte(payload), &wrapped); err == nil {
		for _, item := range wrapped.TestCases {
			if cleaned, ok := workbenchCleanGeneratedTestCaseValues(item.VariableValues, variableNames); ok {
				generatedCases = append(generatedCases, cleaned)
			}
			if len(generatedCases) >= count {
				return generatedCases
			}
		}
	}
	var array []map[string]any
	if err := json.Unmarshal([]byte(payload), &array); err == nil {
		for _, item := range array {
			values := item
			if nested, ok := item["variable_values"].(map[string]any); ok {
				values = nested
			}
			if cleaned, ok := workbenchCleanGeneratedTestCaseValues(values, variableNames); ok {
				generatedCases = append(generatedCases, cleaned)
			}
			if len(generatedCases) >= count {
				return generatedCases
			}
		}
	}
	return generatedCases
}

func workbenchCleanGeneratedTestCaseValues(values map[string]any, variableNames []string) (map[string]any, bool) {
	if len(values) == 0 || len(variableNames) == 0 {
		return nil, false
	}
	cleaned := map[string]any{}
	for _, name := range variableNames {
		rawValue, ok := values[name]
		if !ok {
			return nil, false
		}
		value := strings.TrimSpace(workbenchScalarString(rawValue))
		if value == "" {
			return nil, false
		}
		cleaned[name] = value
	}
	return cleaned, true
}

func workbenchExtractJSONPayload(text string) string {
	text = workbenchStripCodeFence(text)
	if json.Valid([]byte(text)) {
		return text
	}
	for _, pair := range [][2]string{{"{", "}"}, {"[", "]"}} {
		start := strings.Index(text, pair[0])
		end := strings.LastIndex(text, pair[1])
		if start < 0 || end <= start {
			continue
		}
		candidate := strings.TrimSpace(text[start : end+1])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return ""
}

func workbenchStripCodeFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= 1 {
		return strings.Trim(text, "`")
	}
	start := 1
	end := len(lines)
	if strings.TrimSpace(lines[end-1]) == "```" {
		end--
	}
	if start >= end {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func workbenchWriteCompletionStream(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	workbenchWriteSSE(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    "msg_workbench_local",
			"type":  "message",
			"role":  "assistant",
			"model": workbenchDefaultModel,
			"usage": map[string]any{
				"input_tokens":                0,
				"output_tokens":               0,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		},
	})
	workbenchWriteSSE(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	if text != "" {
		workbenchWriteSSE(w, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": text},
		})
	}
	workbenchWriteSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	workbenchWriteSSE(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 0},
	})
	workbenchWriteSSE(w, "message_stop", map[string]any{"type": "message_stop"})
}

func handleWorkbenchCompletions(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	payload, err := readRequiredJSON[map[string]any](r, false)
	if err != nil || payload == nil {
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "request body must match WorkbenchCompletionRequest")
		return
	}
	upstreamBody := workbenchCompletionAnthropicBody(payload)
	if len(chatArrayFromValue(upstreamBody["messages"])) == 0 {
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "at least one non-empty message is required")
		return
	}
	token := proxyMessagesAnthropicToken()
	if token == "" {
		writeProxyMessagesAnthropicError(w, http.StatusInternalServerError, "authentication_error", "ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY is not set")
		return
	}
	endpoint, err := anthropicMessagesEndpoint()
	if err != nil {
		writeProxyMessagesAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	body, err := json.Marshal(upstreamBody)
	if err != nil {
		writeProxyMessagesAnthropicError(w, http.StatusInternalServerError, "api_error", "failed to build Anthropic request")
		return
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		writeProxyMessagesAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	upstreamReq.Header.Set("Accept", "text/event-stream")
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("X-API-Key", token)
	upstreamReq.Header.Set("Anthropic-Version", anthropicAPIVersion)
	if beta := workbenchAnthropicBetaHeader(payload["betas"]); beta != "" {
		upstreamReq.Header.Set("Anthropic-Beta", beta)
	}

	client := &http.Client{Timeout: 0}
	upstreamRes, err := client.Do(upstreamReq)
	if err != nil {
		writeProxyMessagesAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	defer upstreamRes.Body.Close()

	copyProxyMessagesResponseHeaders(w.Header(), upstreamRes.Header)
	if upstreamRes.StatusCode < 200 || upstreamRes.StatusCode >= 300 {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(upstreamRes.StatusCode)
		_, _ = io.Copy(w, upstreamRes.Body)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	proxyMessagesStream(w, upstreamRes.Body)
}

func workbenchCompletionAnthropicBody(payload map[string]any) map[string]any {
	maxTokens := chatPositiveInt(firstNonNil(payload["max_tokens"], payload["max_tokens_to_sample"]))
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}
	variables := workbenchCompletionVariableValues(payload)
	thinking := workbenchCompletionThinking(payload["thinking"], maxTokens)
	body := map[string]any{
		"model":      workbenchCompletionModel(payload),
		"max_tokens": maxTokens,
		"messages":   workbenchCompletionMessages(payload["messages"], variables),
		"stream":     true,
	}
	if systemPrompt := strings.TrimSpace(workbenchSubstituteVariables(chatRawString(payload["system_prompt"]), variables)); systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if thinking != nil {
		body["thinking"] = thinking
	} else if temperature, ok := workbenchNumber(payload["temperature"]); ok {
		body["temperature"] = temperature
	}
	if tools := buildChatCompletionAnthropicTools(chatArrayFromValue(payload["tools"])); len(tools) > 0 {
		body["tools"] = tools
	}
	for _, key := range []string{"tool_choice", "stop_sequences", "metadata", "service_tier", "container", "mcp_servers"} {
		if value, ok := payload[key]; ok {
			body[key] = chatClone(value)
		}
	}
	return body
}

func workbenchCompletionModel(payload map[string]any) string {
	if override := strings.TrimSpace(os.Getenv("WORKBENCH_COMPLETION_MODEL")); override != "" {
		return chatCompletionModel(override)
	}
	return chatCompletionModel(firstNonEmpty(
		chatNormalizeString(payload["model_name"]),
		chatNormalizeString(payload["model"]),
		workbenchDefaultModel,
	))
}

func workbenchCompletionMessages(value any, variables map[string]string) []any {
	out := []any{}
	for _, item := range chatArrayFromValue(value) {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := workbenchCompletionRole(message["role"])
		if role == "" {
			continue
		}
		content := workbenchCompletionContent(message["content"], variables)
		if len(content) == 0 {
			continue
		}
		if len(out) > 0 {
			last, _ := out[len(out)-1].(map[string]any)
			if last != nil && last["role"] == role {
				last["content"] = append(chatArrayFromValue(last["content"]), content...)
				continue
			}
		}
		out = append(out, map[string]any{"role": role, "content": content})
	}
	return out
}

func workbenchCompletionRole(value any) string {
	switch chatNormalizeString(value) {
	case "human", "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return ""
	}
}

func workbenchCompletionContent(value any, variables map[string]string) []any {
	if text := workbenchSubstituteVariables(chatRawString(value), variables); strings.TrimSpace(text) != "" {
		return []any{map[string]any{"type": "text", "text": text}}
	}
	out := []any{}
	for _, item := range chatArrayFromValue(value) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if converted := workbenchCompletionContentBlock(block, variables); converted != nil {
			out = append(out, converted)
		}
	}
	return out
}

func workbenchCompletionContentBlock(block map[string]any, variables map[string]string) map[string]any {
	blockType := chatMapString(block, "type")
	switch blockType {
	case "text":
		text := workbenchSubstituteVariables(chatRawString(block["text"]), variables)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		out := map[string]any{"type": "text", "text": text}
		workbenchCopyIfPresent(out, block, "cache_control")
		workbenchCopyIfPresent(out, block, "citations")
		return out
	case "image", "document":
		out := map[string]any{"type": blockType}
		for _, key := range []string{"source", "cache_control", "title", "context", "citations"} {
			workbenchCopyIfPresent(out, block, key)
		}
		if _, ok := out["source"]; !ok {
			return nil
		}
		return out
	case "tool_use":
		out := map[string]any{"type": "tool_use"}
		for _, key := range []string{"id", "name", "input", "cache_control"} {
			workbenchCopyIfPresent(out, block, key)
		}
		if _, ok := out["input"]; !ok {
			out["input"] = map[string]any{}
		}
		return out
	case "tool_result":
		out := map[string]any{"type": "tool_result"}
		for _, key := range []string{"tool_use_id", "content", "is_error", "cache_control"} {
			workbenchCopyIfPresent(out, block, key)
		}
		if _, ok := out["content"]; !ok {
			out["content"] = []any{}
		}
		return out
	case "thinking":
		out := map[string]any{"type": "thinking"}
		for _, key := range []string{"thinking", "signature"} {
			workbenchCopyIfPresent(out, block, key)
		}
		if strings.TrimSpace(chatRawString(out["thinking"])) == "" {
			return nil
		}
		return out
	case "redacted_thinking":
		out := map[string]any{"type": "redacted_thinking"}
		workbenchCopyIfPresent(out, block, "data")
		if _, ok := out["data"]; !ok {
			return nil
		}
		return out
	case "server_tool_use":
		out := map[string]any{"type": "server_tool_use"}
		for _, key := range []string{"id", "name", "input"} {
			workbenchCopyIfPresent(out, block, key)
		}
		return out
	case "web_search_tool_result":
		out := map[string]any{"type": "web_search_tool_result"}
		for _, key := range []string{"tool_use_id", "content"} {
			workbenchCopyIfPresent(out, block, key)
		}
		return out
	case "example_block":
		if text := workbenchSubstituteVariables(chatRawString(block["text"]), variables); strings.TrimSpace(text) != "" {
			return map[string]any{"type": "text", "text": text}
		}
	}
	return nil
}

func workbenchCompletionThinking(value any, maxTokens int) map[string]any {
	thinking, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	switch chatMapString(thinking, "type") {
	case "", "disabled", "none", "off":
		return nil
	case "enabled":
		budget := chatPositiveInt(firstNonNil(thinking["budget_tokens"], thinking["budgetTokens"]))
		if budget <= 0 {
			budget = min(1024, maxTokens/2)
		}
		if maxTokens > 1 && budget >= maxTokens {
			budget = maxTokens - 1
		}
		if budget <= 0 {
			return nil
		}
		return map[string]any{"type": "enabled", "budget_tokens": budget}
	default:
		return nil
	}
}

func workbenchCompletionVariableValues(payload map[string]any) map[string]string {
	out := map[string]string{}
	for _, field := range []string{"variable_values", "variableValues", "variables"} {
		values, ok := payload[field].(map[string]any)
		if !ok {
			continue
		}
		for key, value := range values {
			if strings.TrimSpace(key) == "" {
				continue
			}
			out[key] = workbenchScalarString(value)
		}
	}
	return out
}

func workbenchSubstituteVariables(text string, variables map[string]string) string {
	if text == "" || len(variables) == 0 {
		return text
	}
	for name, value := range variables {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		text = strings.ReplaceAll(text, "{{"+name+"}}", value)
	}
	return text
}

func workbenchCopyIfPresent(dst map[string]any, src map[string]any, key string) {
	if value, ok := src[key]; ok {
		dst[key] = chatClone(value)
	}
}

func workbenchNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func workbenchScalarString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func workbenchAnthropicBetaHeader(value any) string {
	items := []string{}
	switch v := value.(type) {
	case string:
		items = append(items, v)
	case []string:
		items = append(items, v...)
	case []any:
		for _, item := range v {
			if text := chatNormalizeString(item); text != "" {
				items = append(items, text)
			}
		}
	}
	cleaned := []string{}
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			cleaned = append(cleaned, item)
		}
	}
	return strings.Join(cleaned, ",")
}

func writeWorkbenchTextStream(w http.ResponseWriter, model string, text string, inputTokens int, outputTokens int) {
	if model == "" {
		model = workbenchDefaultModel
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	workbenchWriteSSE(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    "msg_workbench_local",
			"type":  "message",
			"role":  "assistant",
			"model": model,
			"usage": map[string]any{
				"input_tokens":                inputTokens,
				"output_tokens":               0,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		},
	})
	workbenchWriteSSE(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	if text != "" {
		workbenchWriteSSE(w, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": text},
		})
	}
	workbenchWriteSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	workbenchWriteSSE(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": outputTokens},
	})
	workbenchWriteSSE(w, "message_stop", map[string]any{"type": "message_stop"})
}

type workbenchGeneratePromptRequest struct {
	Task               string `json:"task"`
	TargetThinkingMode bool   `json:"target_thinking_mode"`
}

type workbenchGenerateTitleRequest struct {
	MessageContent string `json:"message_content"`
	Model          string `json:"model"`
}

type workbenchGenerateTitleResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func handleWorkbenchGenerateTitle(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	payload, err := readRequiredJSON[workbenchGenerateTitleRequest](r, false)
	if err != nil {
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "request body must match WorkbenchGenerateTitleRequest")
		return
	}
	messageContent := workbenchTitleMessageContent(payload.MessageContent)
	model := workbenchGenerateTitleModel(payload.Model)
	fallbackTitle := workbenchFallbackTitle(messageContent)
	if messageContent == "" {
		writeJSON(w, http.StatusOK, map[string]any{"completion": fallbackTitle})
		return
	}

	title, inputTokens, outputTokens := workbenchGenerateTitleFromAnthropic(r, messageContent, model)
	title = workbenchCleanGeneratedTitle(title)
	if title == "" {
		title = fallbackTitle
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"completion":    title,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
}

func workbenchGenerateTitleFromAnthropic(r *http.Request, messageContent string, model string) (string, int, int) {
	token := proxyMessagesAnthropicToken()
	if token == "" {
		return "", 0, 0
	}
	endpoint, err := anthropicMessagesEndpoint()
	if err != nil {
		return "", 0, 0
	}
	body, err := json.Marshal(workbenchGenerateTitleAnthropicBody(messageContent, model))
	if err != nil {
		return "", 0, 0
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, 0
	}
	upstreamReq.Header.Set("Accept", "application/json")
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("X-API-Key", token)
	upstreamReq.Header.Set("Anthropic-Version", anthropicAPIVersion)

	upstreamRes, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		return "", 0, 0
	}
	defer upstreamRes.Body.Close()
	if upstreamRes.StatusCode < 200 || upstreamRes.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, upstreamRes.Body)
		return "", 0, 0
	}

	var upstream workbenchGenerateTitleResponse
	if err := json.NewDecoder(upstreamRes.Body).Decode(&upstream); err != nil {
		return "", 0, 0
	}
	for _, block := range upstream.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			return block.Text, upstream.Usage.InputTokens, upstream.Usage.OutputTokens
		}
	}
	return "", upstream.Usage.InputTokens, upstream.Usage.OutputTokens
}

func workbenchGenerateTitleAnthropicBody(messageContent string, model string) map[string]any {
	return map[string]any{
		"model":       model,
		"max_tokens":  30,
		"temperature": 0,
		"stream":      false,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": workbenchGenerateTitlePrompt(messageContent),
					},
				},
			},
		},
	}
}

func workbenchGenerateTitleModel(requestModel string) string {
	return firstNonEmpty(
		strings.TrimSpace(os.Getenv("WORKBENCH_GENERATE_TITLE_MODEL")),
		chatNormalizeString(requestModel),
		strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")),
		miscDefaultChatModel,
	)
}

func workbenchGenerateTitlePrompt(messageContent string) string {
	return "Generate a short, concise title (max 6 words) for a Claude Workbench prompt that starts with this message. Reply with ONLY the title, no quotes, punctuation, markdown, or explanation.\n\nMessage:\n" + messageContent
}

func workbenchTitleMessageContent(messageContent string) string {
	return workbenchTruncateRunes(strings.TrimSpace(messageContent), 2000)
}

func workbenchFallbackTitle(messageContent string) string {
	title := strings.Join(strings.Fields(messageContent), " ")
	if title == "" {
		return "Untitled"
	}
	return workbenchCleanGeneratedTitle(workbenchTruncateRunes(title, 50))
}

func workbenchCleanGeneratedTitle(title string) string {
	title = strings.TrimSpace(title)
	for _, line := range strings.Split(title, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			title = trimmed
			break
		}
	}
	title = strings.TrimSpace(strings.Trim(title, "\"'`"))
	title = strings.TrimPrefix(title, "- ")
	title = strings.TrimPrefix(title, "* ")
	title = strings.TrimSpace(strings.TrimRight(title, ".,:;- "))
	return workbenchTruncateRunes(title, 80)
}

func workbenchTruncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func handleWorkbenchGeneratePrompt(w http.ResponseWriter, r *http.Request) {
	if !visibleWorkbenchOrg(w, r) {
		return
	}
	payload, err := readRequiredJSON[workbenchGeneratePromptRequest](r, false)
	if err != nil {
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "request body must match WorkbenchGeneratePromptRequest")
		return
	}
	task := strings.TrimSpace(payload.Task)
	if task == "" {
		writeProxyMessagesAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "task is required")
		return
	}
	token := proxyMessagesAnthropicToken()
	if token == "" {
		log.Printf("workbench generate_prompt fallback reason=no_anthropic_token org=%s task_chars=%d thinking=%t", chi.URLParam(r, "orgUUID"), len([]rune(task)), payload.TargetThinkingMode)
		workbenchWriteGeneratePromptFallbackStream(w, task, payload.TargetThinkingMode)
		return
	}
	endpoint, err := anthropicMessagesEndpoint()
	if err != nil {
		log.Printf("workbench generate_prompt fallback reason=invalid_anthropic_endpoint org=%s err=%v", chi.URLParam(r, "orgUUID"), err)
		workbenchWriteGeneratePromptFallbackStream(w, task, payload.TargetThinkingMode)
		return
	}
	body, err := json.Marshal(workbenchGeneratePromptAnthropicBody(task, payload.TargetThinkingMode))
	if err != nil {
		log.Printf("workbench generate_prompt fallback reason=marshal_request_failed org=%s err=%v", chi.URLParam(r, "orgUUID"), err)
		workbenchWriteGeneratePromptFallbackStream(w, task, payload.TargetThinkingMode)
		return
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		log.Printf("workbench generate_prompt fallback reason=build_upstream_request_failed org=%s endpoint=%s err=%v", chi.URLParam(r, "orgUUID"), endpoint, err)
		workbenchWriteGeneratePromptFallbackStream(w, task, payload.TargetThinkingMode)
		return
	}
	upstreamReq.Header.Set("Accept", "text/event-stream")
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("X-API-Key", token)
	upstreamReq.Header.Set("Anthropic-Version", anthropicAPIVersion)

	log.Printf("workbench generate_prompt upstream_start org=%s endpoint=%s model=%s task_chars=%d thinking=%t", chi.URLParam(r, "orgUUID"), endpoint, workbenchGeneratePromptModel(), len([]rune(task)), payload.TargetThinkingMode)
	upstreamRes, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		log.Printf("workbench generate_prompt fallback reason=upstream_request_failed org=%s endpoint=%s err=%v", chi.URLParam(r, "orgUUID"), endpoint, err)
		workbenchWriteGeneratePromptFallbackStream(w, task, payload.TargetThinkingMode)
		return
	}
	defer upstreamRes.Body.Close()

	if upstreamRes.StatusCode < 200 || upstreamRes.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, upstreamRes.Body)
		log.Printf("workbench generate_prompt fallback reason=upstream_status org=%s endpoint=%s status=%d", chi.URLParam(r, "orgUUID"), endpoint, upstreamRes.StatusCode)
		workbenchWriteGeneratePromptFallbackStream(w, task, payload.TargetThinkingMode)
		return
	}

	log.Printf("workbench generate_prompt upstream_stream org=%s endpoint=%s status=%d content_type=%q", chi.URLParam(r, "orgUUID"), endpoint, upstreamRes.StatusCode, upstreamRes.Header.Get("Content-Type"))
	copyProxyMessagesResponseHeaders(w.Header(), upstreamRes.Header)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	workbenchProxyGeneratePromptStream(w, upstreamRes.Body)
}

func workbenchWriteGeneratePromptFallbackStream(w http.ResponseWriter, task string, targetThinkingMode bool) {
	writeWorkbenchTextStream(w, workbenchGeneratePromptModel(), workbenchGeneratePromptFallbackText(task, targetThinkingMode), 0, 0)
}

func workbenchGeneratePromptFallbackText(task string, targetThinkingMode bool) string {
	task = strings.TrimSpace(strings.Join(strings.Fields(task), " "))
	if task == "" {
		task = "Complete the user's task clearly and accurately."
	}
	task = workbenchTruncateRunes(task, 1000)
	var b strings.Builder
	b.WriteString("<planning>\n")
	b.WriteString("Create a reusable Workbench prompt from the user's task.\n")
	b.WriteString("</planning>\n")
	b.WriteString("<Instructions>\n")
	b.WriteString("You are Claude, an expert assistant.\n\n")
	b.WriteString("Goal\n")
	b.WriteString(task)
	b.WriteString("\n\nInstructions\n")
	b.WriteString("- Understand the user's request and identify the concrete outcome they need.\n")
	b.WriteString("- Use any provided context or input faithfully; do not invent facts that are not supported.\n")
	b.WriteString("- Ask a clarifying question only when the missing detail would materially change the answer.\n")
	b.WriteString("- Produce a polished, directly usable result with clear structure and concise language.\n")
	if targetThinkingMode {
		b.WriteString("- Think through the task privately before answering, but do not reveal hidden chain-of-thought.\n")
	} else {
		b.WriteString("- Include brief reasoning only when it helps the user trust or use the answer.\n")
	}
	b.WriteString("\nOutput\n")
	b.WriteString("- Start with the answer or deliverable, not a preamble.\n")
	b.WriteString("- Use headings or bullets when they improve readability.\n")
	b.WriteString("- Call out assumptions, caveats, and next steps only when relevant.\n")
	b.WriteString("\n</Instructions>")
	return b.String()
}

func workbenchGeneratePromptAnthropicBody(task string, targetThinkingMode bool) map[string]any {
	return map[string]any{
		"model":       workbenchGeneratePromptModel(),
		"max_tokens":  2048,
		"temperature": 0.2,
		"stream":      true,
		"system":      workbenchGeneratePromptSystemPrompt(targetThinkingMode),
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Task:\n" + task + "\n\nWrite the Workbench prompt now.",
					},
				},
			},
		},
	}
}

func workbenchGeneratePromptModel() string {
	return chatCompletionModel(firstNonEmpty(
		strings.TrimSpace(os.Getenv("WORKBENCH_GENERATE_PROMPT_MODEL")),
		strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")),
		miscDefaultChatModel,
	))
}

func workbenchGeneratePromptSystemPrompt(targetThinkingMode bool) string {
	var b strings.Builder
	b.WriteString("You are an expert prompt engineer writing prompts for Claude Workbench.\n")
	b.WriteString("Given a user task, produce one polished prompt that can be pasted directly into Workbench.\n")
	b.WriteString("Return a single XML-style text stream with exactly two top-level sections: <planning>...</planning> followed by <Instructions>...</Instructions>.\n")
	b.WriteString("Keep <planning> concise and use it only for private planning. Put only the reusable Workbench prompt inside <Instructions>. Do not include markdown fences or any text outside those tags.\n")
	b.WriteString("Make the prompt specific, actionable, and self-contained. Preserve the user's intent and constraints.\n")
	b.WriteString("Prefer clear sections, input/output expectations, acceptance criteria, and variable placeholders like {{input}} only when they genuinely help.\n")
	if targetThinkingMode {
		b.WriteString("Optimize the prompt for a Claude run where extended thinking is enabled: ask Claude to reason carefully internally before answering, but do not ask it to reveal hidden reasoning.\n")
	} else {
		b.WriteString("Optimize the prompt for a normal Claude run: ask for concise, high-signal reasoning in the final answer only when useful.\n")
	}
	return b.String()
}

func workbenchProxyGeneratePromptStream(w http.ResponseWriter, body io.Reader) {
	proxyMessagesStream(w, body)
}

func eventNameForPayload(eventName, eventType string) string {
	if strings.TrimSpace(eventName) != "" {
		return eventName
	}
	return eventType
}

func writeRawSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventName, data string) {
	if strings.TrimSpace(eventName) != "" {
		_, _ = w.Write([]byte("event: " + eventName + "\n"))
	}
	if data != "" {
		for _, line := range strings.Split(data, "\n") {
			_, _ = w.Write([]byte("data: " + line + "\n"))
		}
	}
	_, _ = w.Write([]byte("\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func workbenchPromptSummary(r *http.Request, promptID string, workspaceID string, name string) map[string]any {
	createdAt := workbenchDefaultCreatedAt
	updatedAt := workbenchDefaultCreatedAt
	isShared := workbenchPromptShared(r, promptID)
	if record, ok := workbenchStoredPromptRecord(r, promptID); ok {
		if strings.TrimSpace(record.WorkspaceID) != "" {
			workspaceID = record.WorkspaceID
		}
		if strings.TrimSpace(name) == "" {
			name = record.Name
		}
		isShared = record.IsSharedWithWorkspace
		if !record.CreatedAt.IsZero() {
			createdAt = formatJSISOString(record.CreatedAt)
		}
		if !record.UpdatedAt.IsZero() {
			updatedAt = formatJSISOString(record.UpdatedAt)
		}
	}
	name = workbenchPromptName(r, promptID, name)
	return map[string]any{
		"id":                       promptID,
		"created_at":               createdAt,
		"updated_at":               updatedAt,
		"name":                     name,
		"workspace_id":             workspaceID,
		"is_shared_with_workspace": isShared,
		"creator":                  workbenchCreator(r),
	}
}

func workbenchPromptDetail(r *http.Request, promptID string, workspaceID string, name string) map[string]any {
	prompt := workbenchPromptSummary(r, promptID, workspaceID, name)
	prompt["latest_revision"] = workbenchLatestRevision(r, promptID, true, false)
	kvStore := map[string]any{}
	if entry, ok := workbenchStoredKV(r, promptID, "draft_revision"); ok && strings.TrimSpace(entry.Value) != "" {
		kvStore["draft_revision"] = entry.Value
	}
	if promptID == workbenchDefaultPromptID {
		kvStore["examples"] = workbenchDefaultExamples()
	}
	prompt["kv_store"] = kvStore
	return prompt
}

func workbenchDefaultExamples() []any {
	return []any{
		map[string]any{
			"variable_values": map[string]any{"ANIMAL": "Generated ANIMAL example 1"},
			"ideal_output":    "A tiny owl poem.",
		},
		map[string]any{
			"variable_values":    map[string]any{"ANIMAL": "falcon"},
			"ideal_output":       "A swift sky poem.",
			"additional_context": "Keep the tone playful.",
		},
	}
}

func workbenchLatestRevision(r *http.Request, promptID string, includeMessages bool, includeCreator bool) map[string]any {
	if revision, _, ok := workbenchStoredLatestRevision(r, promptID, includeMessages, includeCreator); ok {
		return revision
	}
	return workbenchRevision(r, workbenchDefaultRevisionID, includeMessages, includeCreator)
}

func workbenchStoredLatestRevision(r *http.Request, promptID string, includeMessages bool, includeCreator bool) (map[string]any, string, bool) {
	if record, ok := workbenchStoredPromptRecord(r, promptID); ok && record.LatestRevisionUUID != nil {
		revisionID := strings.TrimSpace(*record.LatestRevisionUUID)
		if revisionID != "" {
			if revision, ok := workbenchStoredRevision(r, promptID, revisionID, includeMessages, includeCreator); ok {
				return revision, revisionID, true
			}
		}
	}
	if latestID, ok := workbenchLocalLatestRevisionIDs.Load(workbenchPromptStoreKey(r, promptID)); ok {
		if revisionID, ok := latestID.(string); ok {
			if revision, ok := workbenchStoredRevision(r, promptID, revisionID, includeMessages, includeCreator); ok {
				return revision, revisionID, true
			}
		}
	}
	return nil, "", false
}

func workbenchRevision(r *http.Request, revisionID string, includeMessages bool, includeCreator bool) map[string]any {
	revision := map[string]any{
		"system_prompt":            "",
		"model_name":               workbenchDefaultModel,
		"variables":                []any{},
		"max_tokens_to_sample":     20000,
		"temperature":              1,
		"thinking":                 map[string]any{"type": "enabled", "budget_tokens": 16000},
		"show_raw_thinking":        false,
		"skip_system_modification": false,
		"is_latest":                true,
		"id":                       revisionID,
		"created_at":               workbenchDefaultCreatedAt,
		"tools":                    []any{},
	}
	if includeMessages {
		revision["messages"] = workbenchDefaultMessages()
	}
	if includeCreator {
		revision["creator"] = workbenchCreator(r)
	}
	return revision
}

func workbenchRevisionFromEvaluations(r *http.Request, revisionID string, includeMessages bool, includeCreator bool) (map[string]any, bool) {
	evaluations := workbenchStoredEvaluationMaps(r, revisionID)
	if len(evaluations) == 0 {
		return nil, false
	}
	variables := workbenchVariableNamesFromEvaluations(evaluations)
	revision := workbenchRevision(r, revisionID, includeMessages, includeCreator)
	revision["variables"] = variables
	if includeMessages {
		revision["messages"] = workbenchMessagesForVariables(variables)
	}
	return revision, true
}

func workbenchCompactRevision(revision map[string]any) map[string]any {
	compact := workbenchCloneMap(revision)
	delete(compact, "messages")
	delete(compact, "is_latest")
	return compact
}

func workbenchRevisionFromBody(r *http.Request, body map[string]any, fallbackID string, includeMessages bool, includeCreator bool) map[string]any {
	revisionID := strings.TrimSpace(workbenchString(body["id"]))
	if revisionID == "" {
		revisionID = fallbackID
	}
	revision := workbenchRevision(r, revisionID, includeMessages, includeCreator)
	revision["created_at"] = formatJSISOString(time.Now())
	workbenchSetStringField(revision, body, "system_prompt")
	workbenchSetStringField(revision, body, "model_name")
	workbenchSetNumberField(revision, body, "max_tokens_to_sample")
	workbenchSetNumberField(revision, body, "temperature")
	workbenchSetBoolField(revision, body, "show_raw_thinking")
	workbenchSetBoolField(revision, body, "skip_system_modification")
	workbenchSetMapField(revision, body, "thinking")
	workbenchSetArrayField(revision, body, "variables")
	workbenchSetArrayField(revision, body, "tools")
	workbenchSetArrayField(revision, body, "messages")
	workbenchNormalizeRevisionVariables(revision)
	revision["is_latest"] = true
	return revision
}

func workbenchStoreRevision(r *http.Request, promptID string, revision map[string]any) error {
	revisionID := strings.TrimSpace(workbenchString(revision["id"]))
	if revisionID == "" {
		return nil
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		if err := store.UpsertWorkbenchRevision(r.Context(), WorkbenchRevisionRecord{
			OrgUUID:      workbenchOrgUUID(r),
			PromptUUID:   strings.TrimSpace(promptID),
			RevisionUUID: revisionID,
			Payload:      workbenchCloneMap(revision),
		}); err != nil {
			return err
		}
		record, err := workbenchPromptRecordForUpsert(r, promptID, "default")
		if err != nil {
			return err
		}
		record.LatestRevisionUUID = &revisionID
		record.DeletedAt = nil
		if _, err := store.UpsertWorkbenchPrompt(r.Context(), record); err != nil {
			return err
		}
	}
	workbenchLocalRevisions.Store(workbenchRevisionStoreKey(r, promptID, revisionID), workbenchCloneMap(revision))
	workbenchLocalLatestRevisionIDs.Store(workbenchPromptStoreKey(r, promptID), revisionID)
	return nil
}

func workbenchStoredRevision(r *http.Request, promptID string, revisionID string, includeMessages bool, includeCreator bool) (map[string]any, bool) {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := store.GetWorkbenchRevision(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(promptID), strings.TrimSpace(revisionID))
		if err == nil && record != nil {
			revision := workbenchCloneMap(record.Payload)
			if !includeMessages {
				delete(revision, "messages")
			}
			if includeCreator {
				revision["creator"] = workbenchCreator(r)
			} else {
				delete(revision, "creator")
			}
			return revision, true
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, false
		}
	}
	value, ok := workbenchLocalRevisions.Load(workbenchRevisionStoreKey(r, promptID, revisionID))
	if !ok {
		return nil, false
	}
	stored, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	revision := workbenchCloneMap(stored)
	if !includeMessages {
		delete(revision, "messages")
	}
	if includeCreator {
		revision["creator"] = workbenchCreator(r)
	} else {
		delete(revision, "creator")
	}
	return revision, true
}

func workbenchPromptStoreKey(r *http.Request, promptID string) string {
	return strings.TrimSpace(chi.URLParam(r, "orgUuid")) + "\x00" + strings.TrimSpace(promptID)
}

func workbenchRevisionStoreKey(r *http.Request, promptID string, revisionID string) string {
	return workbenchPromptStoreKey(r, promptID) + "\x00" + strings.TrimSpace(revisionID)
}

func workbenchKVStoreKey(r *http.Request, promptID string, key string) string {
	return workbenchPromptStoreKey(r, promptID) + "\x00kv\x00" + strings.TrimSpace(key)
}

func workbenchOrgUUID(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "orgUuid"))
}

func workbenchStoredPromptRecord(r *http.Request, promptID string) (*WorkbenchPromptRecord, bool) {
	store := workbenchPersistenceFromRequest(r)
	if store == nil {
		return nil, false
	}
	record, err := store.GetWorkbenchPrompt(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(promptID))
	if err != nil || record == nil {
		return nil, false
	}
	return record, true
}

func workbenchPromptRecordForUpsert(r *http.Request, promptID string, workspaceID string) (WorkbenchPromptRecord, error) {
	promptID = strings.TrimSpace(promptID)
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = "default"
	}
	record := WorkbenchPromptRecord{
		OrgUUID:     workbenchOrgUUID(r),
		PromptUUID:  promptID,
		WorkspaceID: workspaceID,
		Name:        workbenchPromptName(r, promptID, ""),
	}
	if workbenchPromptShared(r, promptID) {
		record.IsSharedWithWorkspace = true
	}
	if latestID, ok := workbenchLocalLatestRevisionIDs.Load(workbenchPromptStoreKey(r, promptID)); ok {
		if revisionID, ok := latestID.(string); ok && strings.TrimSpace(revisionID) != "" {
			record.LatestRevisionUUID = &revisionID
		}
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		current, err := store.GetWorkbenchPrompt(r.Context(), record.OrgUUID, promptID)
		if err == nil && current != nil {
			record = *current
			if strings.TrimSpace(record.WorkspaceID) == "" {
				record.WorkspaceID = workspaceID
			}
			return record, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return WorkbenchPromptRecord{}, err
		}
	}
	return record, nil
}

func workbenchPromptDeleted(r *http.Request, promptID string) (bool, error) {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := store.GetWorkbenchPrompt(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(promptID))
		if err == nil && record != nil {
			return record.DeletedAt != nil, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return false, err
		}
	}
	_, ok := workbenchLocalDeletedPrompts.Load(workbenchPromptStoreKey(r, promptID))
	return ok, nil
}

func workbenchDeletePrompt(r *http.Request, promptID string) error {
	promptID = strings.TrimSpace(promptID)
	if store := workbenchPersistenceFromRequest(r); store != nil {
		if err := store.DeleteWorkbenchPromptState(r.Context(), workbenchOrgUUID(r), promptID); err != nil {
			return err
		}
	}
	promptKey := workbenchPromptStoreKey(r, promptID)
	workbenchLocalDeletedPrompts.Store(promptKey, true)
	workbenchLocalPromptNames.Delete(promptKey)
	workbenchLocalPromptSharing.Delete(promptKey)
	workbenchLocalLatestRevisionIDs.Delete(promptKey)
	workbenchDeleteMapStringPrefix(&workbenchLocalRevisions, promptKey+"\x00")
	workbenchDeleteMapStringPrefix(&workbenchLocalKV, promptKey+"\x00")
	workbenchDeleteMapStringPrefix(&workbenchLocalEvaluations, workbenchEvaluationOrgPrefix(r))
	workbenchDeleteMapStringPrefix(&workbenchLocalGeneratedTestCases, workbenchGeneratedTestCaseStoreKey(r))
	if promptID == workbenchDefaultPromptID {
		return workbenchUndeletePrompt(r, promptID, "default")
	}
	return nil
}

func workbenchUndeletePrompt(r *http.Request, promptID string, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = "default"
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := workbenchPromptRecordForUpsert(r, promptID, workspaceID)
		if err != nil {
			return err
		}
		record.WorkspaceID = workspaceID
		record.DeletedAt = nil
		if _, err := store.UpsertWorkbenchPrompt(r.Context(), record); err != nil {
			return err
		}
	}
	workbenchLocalDeletedPrompts.Delete(workbenchPromptStoreKey(r, promptID))
	return nil
}

func workbenchDeleteMapStringPrefix(values *sync.Map, prefix string) {
	values.Range(func(key any, _ any) bool {
		keyString, ok := key.(string)
		if ok && strings.HasPrefix(keyString, prefix) {
			values.Delete(key)
		}
		return true
	})
}

func writeWorkbenchPromptNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found", "entity": "prompt"})
}

func workbenchStorePromptName(r *http.Request, promptID string, name string) error {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := workbenchPromptRecordForUpsert(r, promptID, "default")
		if err != nil {
			return err
		}
		record.Name = name
		record.DeletedAt = nil
		if _, err := store.UpsertWorkbenchPrompt(r.Context(), record); err != nil {
			return err
		}
	}
	workbenchLocalPromptNames.Store(workbenchPromptStoreKey(r, promptID), name)
	return nil
}

func workbenchStorePromptSharing(r *http.Request, promptID string, shared bool) error {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := workbenchPromptRecordForUpsert(r, promptID, "default")
		if err != nil {
			return err
		}
		record.IsSharedWithWorkspace = shared
		record.DeletedAt = nil
		if _, err := store.UpsertWorkbenchPrompt(r.Context(), record); err != nil {
			return err
		}
	}
	if shared {
		workbenchLocalPromptSharing.Store(workbenchPromptStoreKey(r, promptID), true)
	} else {
		workbenchLocalPromptSharing.Delete(workbenchPromptStoreKey(r, promptID))
	}
	return nil
}

func workbenchPromptName(r *http.Request, promptID string, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	value, ok := workbenchLocalPromptNames.Load(workbenchPromptStoreKey(r, promptID))
	if !ok {
		return fallback
	}
	name, ok := value.(string)
	if !ok {
		return fallback
	}
	return name
}

func workbenchPromptShared(r *http.Request, promptID string) bool {
	value, ok := workbenchLocalPromptSharing.Load(workbenchPromptStoreKey(r, promptID))
	if !ok {
		return false
	}
	shared, ok := value.(bool)
	return ok && shared
}

func workbenchStoreKV(r *http.Request, promptID string, key string, entry workbenchKVEntry) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := workbenchPromptRecordForUpsert(r, promptID, "default")
		if err != nil {
			return err
		}
		record.DeletedAt = nil
		if _, err := store.UpsertWorkbenchPrompt(r.Context(), record); err != nil {
			return err
		}
		if err := store.UpsertWorkbenchKV(r.Context(), WorkbenchKVRecord{
			OrgUUID:    workbenchOrgUUID(r),
			PromptUUID: strings.TrimSpace(promptID),
			Key:        strings.TrimSpace(key),
			Value:      entry.Value,
			Version:    chatClone(entry.Version),
		}); err != nil {
			return err
		}
	}
	workbenchLocalKV.Store(workbenchKVStoreKey(r, promptID, key), entry)
	return nil
}

func workbenchDeleteKV(r *http.Request, promptID string, key string) error {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		if err := store.DeleteWorkbenchKV(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(promptID), strings.TrimSpace(key)); err != nil {
			return err
		}
	}
	workbenchLocalKV.Delete(workbenchKVStoreKey(r, promptID, key))
	return nil
}

func workbenchStoredKV(r *http.Request, promptID string, key string) (workbenchKVEntry, bool) {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := store.GetWorkbenchKV(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(promptID), strings.TrimSpace(key))
		if err == nil && record != nil {
			return workbenchKVEntry{Value: record.Value, Version: chatClone(record.Version)}, true
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return workbenchKVEntry{}, false
		}
	}
	value, ok := workbenchLocalKV.Load(workbenchKVStoreKey(r, promptID, key))
	if !ok {
		return workbenchKVEntry{}, false
	}
	entry, ok := value.(workbenchKVEntry)
	if !ok {
		return workbenchKVEntry{}, false
	}
	return entry, true
}

func workbenchEvaluationFromBody(r *http.Request, body map[string]any, revisionID string) map[string]any {
	evaluationID := strings.TrimSpace(workbenchString(body["id"]))
	if evaluationID == "" {
		evaluationID = "eval_local_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	testCaseID := strings.TrimSpace(workbenchString(body["test_case_id"]))
	if testCaseID == "" {
		testCaseID = evaluationID
	}
	evaluation := map[string]any{
		"id":              evaluationID,
		"revision_id":     revisionID,
		"test_case_id":    testCaseID,
		"variable_values": workbenchMapField(body, "variable_values"),
		"golden_answer":   workbenchNullableField(body, "golden_answer"),
		"completion":      workbenchNullableField(body, "completion"),
		"completion_text": workbenchNullableField(body, "completion_text"),
		"rating":          workbenchNullableField(body, "rating"),
		"created_at":      formatJSISOString(time.Now()),
	}
	if evaluation["completion_text"] == nil {
		evaluation["completion_text"] = workbenchCompletionText(evaluation["completion"])
	}
	return evaluation
}

func workbenchStoreEvaluation(r *http.Request, evaluation map[string]any) error {
	revisionID := strings.TrimSpace(workbenchString(evaluation["revision_id"]))
	if revisionID == "" {
		return nil
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		evaluationID := strings.TrimSpace(workbenchString(evaluation["id"]))
		if evaluationID == "" {
			return nil
		}
		if err := store.UpsertWorkbenchEvaluation(r.Context(), WorkbenchEvaluationRecord{
			OrgUUID:        workbenchOrgUUID(r),
			RevisionUUID:   revisionID,
			EvaluationUUID: evaluationID,
			Payload:        workbenchCloneMap(evaluation),
		}); err != nil {
			return err
		}
	}
	workbenchLocalEvaluationMu.Lock()
	defer workbenchLocalEvaluationMu.Unlock()

	key := workbenchEvaluationStoreKey(r, revisionID)
	evaluations := workbenchEvaluationSliceFromStore(key)
	evaluations = append(evaluations, workbenchCloneMap(evaluation))
	workbenchLocalEvaluations.Store(key, evaluations)
	return nil
}

func workbenchStoredEvaluations(r *http.Request, revisionID string) []any {
	evaluations := workbenchStoredEvaluationMaps(r, revisionID)
	out := make([]any, 0, len(evaluations))
	for _, evaluation := range evaluations {
		out = append(out, workbenchCloneMap(evaluation))
	}
	return out
}

func workbenchStoredEvaluationMaps(r *http.Request, revisionID string) []map[string]any {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		records, err := store.ListWorkbenchEvaluations(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(revisionID))
		if err == nil {
			evaluations := make([]map[string]any, 0, len(records))
			for _, record := range records {
				evaluations = append(evaluations, workbenchCloneMap(record.Payload))
			}
			return evaluations
		}
	}
	workbenchLocalEvaluationMu.Lock()
	defer workbenchLocalEvaluationMu.Unlock()

	return workbenchEvaluationSliceFromStore(workbenchEvaluationStoreKey(r, revisionID))
}

func workbenchUpdateEvaluation(r *http.Request, evaluationID string, body map[string]any) (map[string]any, bool, error) {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := store.GetWorkbenchEvaluation(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(evaluationID))
		if err == nil && record != nil {
			next := workbenchEvaluationWithPatch(r, record.Payload, body)
			revisionID := strings.TrimSpace(workbenchString(next["revision_id"]))
			if revisionID == "" {
				revisionID = record.RevisionUUID
				next["revision_id"] = revisionID
			}
			if err := store.UpsertWorkbenchEvaluation(r.Context(), WorkbenchEvaluationRecord{
				OrgUUID:        record.OrgUUID,
				RevisionUUID:   revisionID,
				EvaluationUUID: record.EvaluationUUID,
				Payload:        next,
			}); err != nil {
				return nil, false, err
			}
			return workbenchCloneMap(next), true, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
	}
	workbenchLocalEvaluationMu.Lock()
	defer workbenchLocalEvaluationMu.Unlock()

	orgPrefix := workbenchEvaluationOrgPrefix(r)
	var updated map[string]any
	var updatedKey string
	var updatedEvaluations []map[string]any
	workbenchLocalEvaluations.Range(func(key any, value any) bool {
		keyString, ok := key.(string)
		if !ok || !strings.HasPrefix(keyString, orgPrefix) {
			return true
		}
		evaluations, ok := value.([]map[string]any)
		if !ok {
			return true
		}
		for idx, evaluation := range evaluations {
			if workbenchString(evaluation["id"]) != evaluationID {
				continue
			}
			next := workbenchEvaluationWithPatch(r, evaluation, body)
			evaluations[idx] = workbenchCloneMap(next)
			updated = next
			updatedKey = keyString
			updatedEvaluations = evaluations
			return false
		}
		return true
	})
	if updated == nil {
		return nil, false, nil
	}
	workbenchLocalEvaluations.Store(updatedKey, updatedEvaluations)
	return workbenchCloneMap(updated), true, nil
}

func workbenchEvaluationWithPatch(r *http.Request, evaluation map[string]any, body map[string]any) map[string]any {
	next := workbenchCloneMap(evaluation)
	for _, field := range []string{"completion", "completion_text", "rating", "golden_answer", "variable_values"} {
		if value, ok := body[field]; ok {
			if field == "variable_values" {
				variableValues := workbenchMapField(body, field)
				if generatedValues, ok := workbenchTakeGeneratedTestCase(r, variableValues); ok {
					variableValues = generatedValues
				}
				next[field] = variableValues
			} else {
				next[field] = chatClone(value)
			}
		}
	}
	if _, ok := body["completion_text"]; !ok && next["completion_text"] == nil {
		next["completion_text"] = workbenchCompletionText(next["completion"])
	}
	return next
}

func workbenchDeleteEvaluation(r *http.Request, evaluationID string) (map[string]any, bool, error) {
	if strings.TrimSpace(evaluationID) == "" {
		return nil, false, nil
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		record, err := store.DeleteWorkbenchEvaluation(r.Context(), workbenchOrgUUID(r), strings.TrimSpace(evaluationID))
		if err == nil && record != nil {
			return workbenchCloneMap(record.Payload), true, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
	}
	workbenchLocalEvaluationMu.Lock()
	defer workbenchLocalEvaluationMu.Unlock()

	orgPrefix := workbenchEvaluationOrgPrefix(r)
	var deleted map[string]any
	var deletedKey string
	var remaining []map[string]any
	workbenchLocalEvaluations.Range(func(key any, value any) bool {
		keyString, ok := key.(string)
		if !ok || !strings.HasPrefix(keyString, orgPrefix) {
			return true
		}
		evaluations, ok := value.([]map[string]any)
		if !ok {
			return true
		}
		for idx, evaluation := range evaluations {
			if workbenchString(evaluation["id"]) != evaluationID {
				continue
			}
			deleted = workbenchCloneMap(evaluation)
			deletedKey = keyString
			remaining = make([]map[string]any, 0, len(evaluations)-1)
			remaining = append(remaining, evaluations[:idx]...)
			remaining = append(remaining, evaluations[idx+1:]...)
			return false
		}
		return true
	})
	if deleted == nil {
		return nil, false, nil
	}
	if len(remaining) == 0 {
		workbenchLocalEvaluations.Delete(deletedKey)
	} else {
		workbenchLocalEvaluations.Store(deletedKey, remaining)
	}
	return deleted, true, nil
}

func workbenchEvaluationSliceFromStore(key string) []map[string]any {
	value, ok := workbenchLocalEvaluations.Load(key)
	if !ok {
		return nil
	}
	evaluations, ok := value.([]map[string]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(evaluations))
	for _, evaluation := range evaluations {
		out = append(out, workbenchCloneMap(evaluation))
	}
	return out
}

func workbenchEvaluationRevisionIDs(r *http.Request) []string {
	if store := workbenchPersistenceFromRequest(r); store != nil {
		revisionIDs, err := store.ListWorkbenchEvaluationRevisionIDs(r.Context(), workbenchOrgUUID(r))
		if err == nil {
			return revisionIDs
		}
	}
	workbenchLocalEvaluationMu.Lock()
	defer workbenchLocalEvaluationMu.Unlock()

	prefix := workbenchEvaluationOrgPrefix(r)
	seen := map[string]bool{}
	workbenchLocalEvaluations.Range(func(key any, _ any) bool {
		keyString, ok := key.(string)
		if !ok || !strings.HasPrefix(keyString, prefix) {
			return true
		}
		revisionID := strings.TrimSpace(strings.TrimPrefix(keyString, prefix))
		if revisionID != "" {
			seen[revisionID] = true
		}
		return true
	})
	revisionIDs := make([]string, 0, len(seen))
	for revisionID := range seen {
		revisionIDs = append(revisionIDs, revisionID)
	}
	sort.Strings(revisionIDs)
	return revisionIDs
}

func workbenchVariableNamesFromEvaluations(evaluations []map[string]any) []any {
	seen := map[string]bool{}
	for _, evaluation := range evaluations {
		variableValues := workbenchMapField(evaluation, "variable_values")
		for name := range variableValues {
			name = strings.TrimSpace(name)
			if name != "" {
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	variables := make([]any, 0, len(names))
	for _, name := range names {
		variables = append(variables, name)
	}
	return variables
}

func workbenchEvaluationOrgPrefix(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "orgUuid")) + "\x00eval\x00"
}

func workbenchEvaluationStoreKey(r *http.Request, revisionID string) string {
	return workbenchEvaluationOrgPrefix(r) + strings.TrimSpace(revisionID)
}

func workbenchGeneratedTestCaseStoreKey(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "orgUuid")) + "\x00generated_test_cases"
}

func workbenchStoreGeneratedTestCase(r *http.Request, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		if err := store.AppendWorkbenchGeneratedTestCase(r.Context(), workbenchOrgUUID(r), workbenchCloneMap(values)); err != nil {
			return err
		}
	}
	workbenchLocalGeneratedTestCaseMu.Lock()
	defer workbenchLocalGeneratedTestCaseMu.Unlock()

	key := workbenchGeneratedTestCaseStoreKey(r)
	queue, _ := workbenchLocalGeneratedTestCases.Load(key)
	generated, _ := queue.([]map[string]any)
	generated = append(generated, workbenchCloneMap(values))
	if len(generated) > 10 {
		generated = generated[len(generated)-10:]
	}
	workbenchLocalGeneratedTestCases.Store(key, generated)
	return nil
}

func workbenchTakeGeneratedTestCase(r *http.Request, requested map[string]any) (map[string]any, bool) {
	if !workbenchShouldUseGeneratedTestCase(requested) {
		return nil, false
	}
	if store := workbenchPersistenceFromRequest(r); store != nil {
		values, ok, err := store.TakeWorkbenchGeneratedTestCase(r.Context(), workbenchOrgUUID(r), requested)
		if err == nil && ok {
			return values, true
		}
	}
	workbenchLocalGeneratedTestCaseMu.Lock()
	defer workbenchLocalGeneratedTestCaseMu.Unlock()

	key := workbenchGeneratedTestCaseStoreKey(r)
	value, ok := workbenchLocalGeneratedTestCases.Load(key)
	if !ok {
		return nil, false
	}
	generated, ok := value.([]map[string]any)
	if !ok {
		return nil, false
	}
	for idx, candidate := range generated {
		if !workbenchGeneratedTestCaseMatches(candidate, requested) {
			continue
		}
		next := append(generated[:idx:idx], generated[idx+1:]...)
		if len(next) == 0 {
			workbenchLocalGeneratedTestCases.Delete(key)
		} else {
			workbenchLocalGeneratedTestCases.Store(key, next)
		}
		return workbenchCloneMap(candidate), true
	}
	return nil, false
}

func workbenchShouldUseGeneratedTestCase(requested map[string]any) bool {
	if len(requested) == 0 {
		return false
	}
	for _, value := range requested {
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(typed) != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func workbenchGeneratedTestCaseMatches(candidate map[string]any, requested map[string]any) bool {
	if len(candidate) == 0 {
		return false
	}
	for name := range requested {
		if _, ok := candidate[name]; !ok {
			return false
		}
	}
	return true
}

func workbenchMapField(body map[string]any, field string) map[string]any {
	if body == nil {
		return map[string]any{}
	}
	value, ok := body[field].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return workbenchCloneMap(value)
}

func workbenchNullableField(body map[string]any, field string) any {
	if body == nil {
		return nil
	}
	value, ok := body[field]
	if !ok {
		return nil
	}
	return chatClone(value)
}

func workbenchTestCaseCount(body map[string]any) int {
	count := 1
	switch value := body["num_testcases"].(type) {
	case float64:
		count = int(value)
	case int:
		count = value
	case json.Number:
		if parsed, err := strconv.Atoi(value.String()); err == nil {
			count = parsed
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			count = parsed
		}
	}
	if count < 1 {
		return 1
	}
	if count > 20 {
		return 20
	}
	return count
}

func workbenchGeneratedTestCaseText(body map[string]any, index int) string {
	return workbenchGeneratedTestCaseTextFromValues(workbenchGeneratedVariableValues(body, index))
}

func workbenchGeneratedTestCaseTextFromValues(values map[string]any) string {
	var text strings.Builder
	text.WriteString("<planning>Generated local Workbench test case.</planning>\n")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		text.WriteString("<")
		text.WriteString(name)
		text.WriteString(">")
		text.WriteString(workbenchString(values[name]))
		text.WriteString("</")
		text.WriteString(name)
		text.WriteString(">\n")
	}
	return text.String()
}

func workbenchGeneratedVariableValues(body map[string]any, index int) map[string]any {
	values := map[string]any{}
	for _, name := range workbenchVariableNamesFromPayload(body) {
		values[name] = workbenchGeneratedVariableValue(name, index)
	}
	return values
}

func workbenchGeneratedVariableValue(name string, index int) string {
	label := strings.ReplaceAll(strings.TrimSpace(name), "_", " ")
	if label == "" {
		label = "input"
	}
	switch strings.ToLower(name) {
	case "complaint_email":
		return "Customer reports that order #" + strconv.Itoa(1000+index) + " arrived damaged and asks for a quick replacement."
	case "email":
		return "customer" + strconv.Itoa(index) + "@example.com"
	case "name", "customer_name":
		return "Alex " + strconv.Itoa(index)
	default:
		return "Generated " + label + " example " + strconv.Itoa(index)
	}
}

func workbenchVariableNamesFromPayload(body map[string]any) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case string:
			for _, name := range workbenchVariableNamesFromText(typed) {
				add(name)
			}
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			if variable, ok := typed["name"].(string); ok {
				add(variable)
			}
			if variableValues, ok := typed["variable_values"].(map[string]any); ok {
				for name := range variableValues {
					add(name)
				}
			}
			for _, value := range typed {
				visit(value)
			}
		}
	}
	visit(body["prompt"])
	visit(body["system_prompt"])
	visit(body["messages"])
	visit(body["variables"])
	visit(body["examples"])
	visit(body["existing_examples"])
	return names
}

func workbenchNormalizeRevisionVariables(revision map[string]any) {
	if len(chatArrayFromValue(revision["variables"])) > 0 {
		return
	}
	names := workbenchVariableNamesFromPayload(revision)
	if len(names) == 0 {
		return
	}
	variables := make([]any, 0, len(names))
	for _, name := range names {
		variables = append(variables, name)
	}
	revision["variables"] = variables
}

func workbenchNormalizeDraftRevisionValue(value string) string {
	var revision map[string]any
	if err := json.Unmarshal([]byte(value), &revision); err != nil {
		return value
	}
	workbenchNormalizeRevisionVariables(revision)
	encoded, err := json.Marshal(revision)
	if err != nil {
		return value
	}
	return string(encoded)
}

func workbenchVariableNamesFromText(text string) []string {
	var names []string
	for {
		start := strings.Index(text, "{{")
		if start < 0 {
			return names
		}
		text = text[start+2:]
		end := strings.Index(text, "}}")
		if end < 0 {
			return names
		}
		name := strings.TrimSpace(text[:end])
		if name != "" && !strings.ContainsAny(name, "{} \t\r\n") {
			names = append(names, name)
		}
		text = text[end+2:]
	}
}

func workbenchCompletionText(value any) any {
	var blocks []any
	switch completion := value.(type) {
	case map[string]any:
		blocks = chatArrayFromValue(completion["content"])
	case []any:
		blocks = completion
	default:
		return nil
	}
	var text strings.Builder
	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok || chatMapString(block, "type") != "text" {
			continue
		}
		text.WriteString(chatRawString(block["text"]))
	}
	if text.Len() == 0 {
		return nil
	}
	return text.String()
}

func workbenchKVValueFromBody(body map[string]any) (string, bool) {
	for _, field := range []string{"value", "new_value", "draft_revision"} {
		if value, ok := body[field]; ok {
			return workbenchMarshalKVValue(value), true
		}
	}
	return "", false
}

func workbenchMarshalKVValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func workbenchDraftRevisionShouldClear(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == "null"
}

func workbenchDraftRevisionHasContent(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	var revision map[string]any
	if err := json.Unmarshal([]byte(value), &revision); err != nil {
		return true
	}
	if strings.TrimSpace(workbenchString(revision["system_prompt"])) != "" {
		return true
	}
	messages, ok := revision["messages"].([]any)
	if !ok {
		return false
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for _, blockItem := range content {
			block, ok := blockItem.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(workbenchString(block["text"])) != "" {
				return true
			}
		}
	}
	return false
}

func workbenchSetStringField(dst map[string]any, src map[string]any, field string) {
	if value, ok := src[field].(string); ok {
		dst[field] = value
	}
}

func workbenchSetNumberField(dst map[string]any, src map[string]any, field string) {
	switch value := src[field].(type) {
	case float64, float32, int, int64, json.Number:
		dst[field] = value
	}
}

func workbenchSetBoolField(dst map[string]any, src map[string]any, field string) {
	if value, ok := src[field].(bool); ok {
		dst[field] = value
	}
}

func workbenchSetMapField(dst map[string]any, src map[string]any, field string) {
	if value, ok := src[field].(map[string]any); ok {
		dst[field] = value
	}
}

func workbenchSetArrayField(dst map[string]any, src map[string]any, field string) {
	if value, ok := src[field].([]any); ok {
		dst[field] = value
	}
}

func workbenchString(value any) string {
	text, _ := value.(string)
	return text
}

func workbenchCloneMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return map[string]any{}
	}
	return cloned
}

func workbenchDraftRevisionString() string {
	payload := map[string]any{
		"system_prompt":            "",
		"model_name":               workbenchDefaultModel,
		"variables":                []any{},
		"max_tokens_to_sample":     20000,
		"thinking":                 map[string]any{"type": "enabled", "budget_tokens": 16000},
		"show_raw_thinking":        false,
		"skip_system_modification": false,
		"is_latest":                true,
		"id":                       workbenchDefaultRevisionID,
		"created_at":               workbenchDefaultCreatedAt,
		"tools":                    []any{},
		"messages":                 workbenchDefaultMessages(),
	}
	return workbenchRevisionString(payload)
}

func workbenchPromptDraftRevisionString(r *http.Request, promptID string) string {
	if entry, ok := workbenchStoredKV(r, promptID, "draft_revision"); ok && strings.TrimSpace(entry.Value) != "" {
		return entry.Value
	}
	return workbenchRevisionString(workbenchLatestRevision(r, promptID, true, false))
}

func workbenchRevisionString(revision map[string]any) string {
	encoded, _ := json.Marshal(revision)
	return string(encoded)
}

func workbenchDefaultMessages() []any {
	return []any{
		map[string]any{"role": "human", "content": []any{map[string]any{"type": "text", "text": ""}}},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": ""}}},
	}
}

func workbenchMessagesForVariables(variables []any) []any {
	messages := workbenchDefaultMessages()
	if len(variables) == 0 {
		return messages
	}
	lines := []string{"Draft a professional response using these inputs:"}
	for _, variable := range variables {
		name := strings.TrimSpace(workbenchString(variable))
		if name != "" {
			lines = append(lines, name+": {{"+name+"}}")
		}
	}
	if len(lines) == 1 {
		return messages
	}
	human, _ := messages[0].(map[string]any)
	content, _ := human["content"].([]any)
	block, _ := content[0].(map[string]any)
	block["text"] = strings.Join(lines, "\n")
	return messages
}

func workbenchCreator(r *http.Request) map[string]any {
	auth := authFromContext(r.Context())
	if auth == nil {
		return map[string]any{"tagged_id": "", "uuid": "", "full_name": "", "email_address": ""}
	}
	fullName := platformClaudeOptionalStringValue(auth.Account.FullName)
	if fullName == "" {
		fullName = platformClaudeOptionalStringValue(auth.Account.DisplayName)
	}
	if fullName == "" {
		fullName = strings.TrimSpace(strings.Split(auth.Account.EmailAddress, "@")[0])
	}
	return map[string]any{
		"tagged_id":     auth.Account.TaggedID,
		"uuid":          auth.Account.UUID,
		"full_name":     fullName,
		"email_address": auth.Account.EmailAddress,
	}
}

func workbenchBootstrapCreator(r *http.Request) (map[string]any, bool) {
	if r == nil || !isPlatformClaudeHost(r.Host) || strings.TrimSpace(r.Header.Get("Cookie")) == "" {
		return nil, false
	}
	orgUUID := workbenchOrgUUID(r)
	if orgUUID == "" {
		return nil, false
	}
	return fetchWorkbenchBootstrapCreator(r, orgUUID)
}

func fetchWorkbenchBootstrapCreator(r *http.Request, orgUUID string) (map[string]any, bool) {
	baseURL := strings.TrimRight(firstNonEmpty(os.Getenv("PLATFORM_BOOTSTRAP_BASE_URL"), "http://127.0.0.1:8081"), "/")
	if baseURL == "" {
		return nil, false
	}
	endpoint, err := url.Parse(baseURL + "/api/bootstrap/" + url.PathEscape(orgUUID) + "/app_start")
	if err != nil {
		return nil, false
	}
	query := endpoint.Query()
	query.Set("statsig_hashing_algorithm", "djb2")
	query.Set("growthbook_format", "sdk")
	query.Set("include_system_prompts", "false")
	endpoint.RawQuery = query.Encode()

	ctx, cancel := context.WithTimeout(r.Context(), 700*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, false
	}
	req.Host = r.Host
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", r.Header.Get("Cookie"))
	if userAgent := strings.TrimSpace(r.UserAgent()); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var body struct {
		Account map[string]any `json:"account"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, false
	}
	account := normalizeWorkbenchCreatorAccount(body.Account)
	if strings.TrimSpace(workbenchString(account["tagged_id"])) == "" {
		return nil, false
	}
	return account, true
}

func normalizeWorkbenchCreatorAccount(account map[string]any) map[string]any {
	if account == nil {
		return nil
	}
	out := map[string]any{
		"tagged_id":     strings.TrimSpace(workbenchString(account["tagged_id"])),
		"uuid":          strings.TrimSpace(workbenchString(account["uuid"])),
		"full_name":     strings.TrimSpace(workbenchString(account["full_name"])),
		"email_address": strings.TrimSpace(workbenchString(account["email_address"])),
	}
	if out["full_name"] == "" {
		out["full_name"] = strings.TrimSpace(workbenchString(account["display_name"]))
	}
	if out["full_name"] == "" {
		out["full_name"] = out["email_address"]
	}
	return out
}

func workbenchModel(modelName string, displayName string, rateLimitGroup string, maxTokens int, maxOutputTokens int, latest bool, thinking bool, effort bool) map[string]any {
	model := map[string]any{
		"model_name":                modelName,
		"bedrock_name":              "",
		"vertex_name":               modelName,
		"supports_system_prompt":    true,
		"max_tokens":                maxTokens,
		"max_output_tokens":         maxOutputTokens,
		"warning_tokens":            90000,
		"rate_limit_model_group":    rateLimitGroup,
		"rate_limit_display_name":   displayName,
		"is_deprecated":             false,
		"is_latest":                 latest,
		"supports_images":           true,
		"supports_thinking":         thinking,
		"supports_auto_thinking":    thinking,
		"supports_documents":        true,
		"supports_tool_use":         true,
		"supported_server_tools":    []any{"web_search_20250305"},
		"supports_prompt_caching":   true,
		"supports_thinking_display": thinking,
		"default_thinking_display":  "omitted",
	}
	if effort {
		model["supported_effort_levels"] = []any{"low", "medium", "high", "xhigh", "max"}
	}
	return model
}

func workbenchStandardRateLimits() []any {
	return []any{
		platformClaudeRateLimit("input_tokens_per_minute_cache_aware", 10000),
		platformClaudeRateLimit("output_tokens_per_minute", 4000),
		platformClaudeRateLimit("requests_per_minute", 5),
	}
}

func workbenchStandardRateLimitMaps() []map[string]any {
	return []map[string]any{
		platformClaudeRateLimit("input_tokens_per_minute_cache_aware", 10000),
		platformClaudeRateLimit("output_tokens_per_minute", 4000),
		platformClaudeRateLimit("requests_per_minute", 5),
	}
}

func platformClaudeRateLimit(limitType string, value int) map[string]any {
	return map[string]any{"type": limitType, "value": value, "multiplier_config": nil}
}

func workbenchPromptIDFromRequest(r *http.Request) string {
	if promptID := strings.TrimSpace(chi.URLParam(r, "promptUuid")); promptID != "" && promptID != "new" {
		return promptID
	}
	return workbenchDefaultPromptID
}

func workbenchRevisionIDFromRequest(r *http.Request) string {
	if revisionID := strings.TrimSpace(chi.URLParam(r, "revisionUuid")); revisionID != "" {
		return revisionID
	}
	return workbenchDefaultRevisionID
}

func workbenchWriteSSE(w http.ResponseWriter, event string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: " + string(body) + "\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
