package evaluations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxTaskCases = 200

type taskWriteRequest struct {
	Name          string          `json:"name"`
	Mode          string          `json:"mode"`
	AgentID       string          `json:"agent_id"`
	AgentVersion  int             `json:"agent_version"`
	EnvironmentID string          `json:"environment_id"`
	DatasetID     string          `json:"dataset_id"`
	RubricID      string          `json:"rubric_id"`
	Focus         string          `json:"focus"`
	VaultIDs      []string        `json:"vault_ids"`
	Resources     json.RawMessage `json:"resources"`
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[taskWriteRequest](w, r, 1<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	task, items, err := h.prepareTask(r.Context(), principal, *body)
	if err != nil {
		return err
	}
	created, err := h.store.CreateEvaluationTask(r.Context(), task, items)
	if err != nil {
		return mapStoreError(err, taskNotFoundMessage, createFailedMessage)
	}
	h.startItems(r.Context(), principal, created.Task, created.Items)
	detail, err := h.finishStartup(r.Context(), principal, created.Task.ExternalID)
	if err != nil {
		return err
	}
	return h.writeTaskDetail(w, r, principal, detail)
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) error {
	principal, offset, limit, err := h.page(r)
	if err != nil {
		return err
	}
	rows, err := h.store.ListEvaluationTasks(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, offset, limit+1)
	if err != nil {
		return internalFailure(loadFailedMessage, err)
	}
	next := nextPage(offset, limit, len(rows))
	if len(rows) > limit {
		rows = rows[:limit]
	}
	data, err := h.taskListResponses(r.Context(), principal, rows)
	if err != nil {
		return err
	}
	writePage(w, data, next)
	return nil
}

func (h *Handler) taskListResponses(ctx context.Context, principal auth.Principal, tasks []db.EvaluationTask) ([]taskResponse, error) {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.UUID)
	}
	items, scores, err := h.store.ListEvaluationTaskChildren(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, ids)
	if err != nil {
		return nil, internalFailure(loadFailedMessage, err)
	}
	groupedItems := map[string][]db.EvaluationTaskItem{}
	groupedScores := map[string][]db.EvaluationTaskScore{}
	for _, item := range items {
		groupedItems[item.TaskUUID] = append(groupedItems[item.TaskUUID], item)
	}
	for _, score := range scores {
		groupedScores[score.TaskUUID] = append(groupedScores[score.TaskUUID], score)
	}
	data := make([]taskResponse, 0, len(tasks))
	for _, task := range tasks {
		data = append(data, taskJSON(db.EvaluationTaskDetail{
			Task: task, Items: groupedItems[task.UUID], Scores: groupedScores[task.UUID],
		}, false))
	}
	return data, nil
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	detail, err := h.store.GetEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "taskId"))
	if err != nil {
		return mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
	}
	return h.writeTaskDetail(w, r, principal, detail)
}

func (h *Handler) writeTaskDetail(w http.ResponseWriter, r *http.Request, principal auth.Principal, detail db.EvaluationTaskDetail) error {
	response, err := h.taskDetailJSON(r.Context(), principal, detail)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) cancelTask(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	taskID := chi.URLParam(r, "taskId")
	detail, err := h.store.GetEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
	if err != nil {
		return mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
	}
	if detail.Task.Status == "completed" || detail.Task.Status == "failed" || detail.Task.Status == "cancelled" {
		return invalidState("This task can no longer be cancelled", nil)
	}
	now := time.Now().UTC()
	if err := h.store.CancelPendingEvaluationItems(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, detail.Task.UUID, now); err != nil {
		return internalFailure(createFailedMessage, err)
	}
	if err := h.store.UpdateEvaluationTaskStatus(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, taskID, "cancelled", "", now); err != nil {
		return internalFailure(createFailedMessage, err)
	}
	return h.writeTask(w, r, principal, taskID)
}

func (h *Handler) retryTask(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	taskID := chi.URLParam(r, "taskId")
	detail, err := h.store.GetEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
	if err != nil {
		return mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
	}
	if detail.Task.Status != "completed" && detail.Task.Status != "failed" {
		return invalidState(retryUnavailableMessage, nil)
	}
	retried, err := h.store.RetryEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, detail.Task.UUID, latestRound(detail.Items), time.Now().UTC())
	if err != nil {
		return retryStoreError(err)
	}
	if retried.ResetItems > 0 {
		reloaded, err := h.store.GetEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
		if err != nil {
			return mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
		}
		h.startItems(r.Context(), principal, reloaded.Task, reloaded.Items)
	}
	h.requestSettle(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
	return h.writeTask(w, r, principal, taskID)
}

func (h *Handler) prepareTask(ctx context.Context, principal auth.Principal, body taskWriteRequest) (db.EvaluationTask, []db.EvaluationTaskItem, error) {
	name, err := normalizeName(body.Name)
	if err != nil {
		return db.EvaluationTask{}, nil, err
	}
	mode, err := normalizeTaskMode(body)
	if err != nil {
		return db.EvaluationTask{}, nil, err
	}
	agentID := strings.TrimSpace(body.AgentID)
	environmentID := strings.TrimSpace(body.EnvironmentID)
	datasetID := strings.TrimSpace(body.DatasetID)
	rubricID := strings.TrimSpace(body.RubricID)
	if agentID == "" {
		return db.EvaluationTask{}, nil, invalidArgument(agentRequiredMessage, nil)
	}
	if environmentID == "" {
		return db.EvaluationTask{}, nil, invalidArgument(environmentRequiredMessage, nil)
	}
	if datasetID == "" {
		return db.EvaluationTask{}, nil, invalidArgument(datasetRequiredMessage, nil)
	}
	if rubricID == "" {
		return db.EvaluationTask{}, nil, invalidArgument(rubricIDRequiredMessage, nil)
	}
	agent, err := h.loadAgent(ctx, principal.WorkspaceUUID, agentID, body.AgentVersion)
	if err != nil {
		return db.EvaluationTask{}, nil, err
	}
	environment, err := h.store.GetEnvironment(ctx, principal.WorkspaceUUID, environmentID)
	if err != nil {
		return db.EvaluationTask{}, nil, mapStoreError(err, "Environment not found", loadFailedMessage)
	}
	if environment.ArchivedAt != nil {
		return db.EvaluationTask{}, nil, invalidState(archivedEnvironmentMessage, nil)
	}
	cases, err := h.store.ListEvaluationCases(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
	if err != nil {
		return db.EvaluationTask{}, nil, mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
	}
	if len(cases) == 0 {
		return db.EvaluationTask{}, nil, invalidArgument(casesRequiredMessage, nil)
	}
	if len(cases) > maxTaskCases {
		return db.EvaluationTask{}, nil, invalidArgument(casesTooManyMessage, nil)
	}
	dataset, err := h.store.GetEvaluationDataset(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
	if err != nil {
		return db.EvaluationTask{}, nil, mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
	}
	rubric, err := h.store.GetEvaluationRubric(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, rubricID)
	if err != nil {
		return db.EvaluationTask{}, nil, mapStoreError(err, rubricNotFoundMessage, loadFailedMessage)
	}
	judgeModel, err := h.judgeModel(ctx, principal, rubric.Model)
	if err != nil {
		return db.EvaluationTask{}, nil, err
	}
	resources, err := normalizeResources(body.Resources)
	if err != nil {
		return db.EvaluationTask{}, nil, err
	}
	_, vaultJSON, err := normalizeVaults(body.VaultIDs)
	if err != nil {
		return db.EvaluationTask{}, nil, err
	}
	taskID, err := ids.New("evaltask_")
	if err != nil {
		return db.EvaluationTask{}, nil, internalFailure(createFailedMessage, err)
	}
	now := time.Now().UTC()
	baseline := ""
	if agent.System != nil {
		baseline = *agent.System
	}
	noted := rubricWithNotes(rubric.Body, body.Focus)
	if len(noted) > maxRubricLength {
		return db.EvaluationTask{}, nil, invalidArgument(rubricTooLongMessage, nil)
	}
	task := db.EvaluationTask{
		UUID: uuid.NewString(), ExternalID: taskID, OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		CreatedByUserUUID: principal.UserUUID, CreatedByAPIKeyUUID: principal.APIKeyUUID, Name: name, Mode: mode, Status: "pending",
		AgentExternalID: agent.ExternalID, AgentVersion: agent.CurrentVersion, EnvironmentExternalID: environment.ExternalID,
		DatasetExternalID: dataset.ExternalID, DatasetVersionExternalID: dataset.VersionExternalID, RubricExternalID: rubric.ExternalID,
		RubricVersionExternalID: rubric.VersionExternalID, PassScore: rubric.PassScore, RubricBody: noted, RubricModel: judgeModel,
		Strategy: "", LowScoreThreshold: 60, OptimizeSystem: false, BaselineSystem: baseline, VaultIDs: vaultJSON, Resources: resources, CreatedAt: now,
	}
	items := make([]db.EvaluationTaskItem, 0, len(cases))
	for _, item := range cases {
		externalID, err := ids.New("evalitem_")
		if err != nil {
			return db.EvaluationTask{}, nil, internalFailure(createFailedMessage, err)
		}
		items = append(items, db.EvaluationTaskItem{
			UUID: uuid.NewString(), ExternalID: externalID, CaseExternalID: item.ExternalID, Round: 1, Position: item.Position,
			Query: item.Query, ExpectedAnswer: item.ExpectedAnswer, ExpectedToolTrace: item.ExpectedToolTrace, Acceptance: item.Acceptance,
			Inputs: item.Inputs, Status: "pending",
		})
	}
	return task, items, nil
}

func (h *Handler) judgeModel(ctx context.Context, principal auth.Principal, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	resolver, ok := h.model.(defaultModelResolver)
	if !ok {
		return "", nil
	}
	model, found, err := resolver.DefaultModel(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return "", internalFailure(loadFailedMessage, err)
	}
	if !found {
		return "", unavailable(modelMissingMessage, errModelMissing)
	}
	return model, nil
}

func rubricWithNotes(body, focus string) string {
	text := strings.TrimSpace(focus)
	if text == "" {
		return body
	}
	return body + "\n\n评测关注项：" + text
}

func normalizeTaskMode(body taskWriteRequest) (string, error) {
	mode := strings.TrimSpace(body.Mode)
	if mode == "" {
		mode = "evaluate"
	}
	if mode != "evaluate" {
		return "", invalidArgument(modeInvalidMessage, nil)
	}
	return mode, nil
}

func (h *Handler) loadAgent(ctx context.Context, workspaceUUID, agentID string, version int) (db.Agent, error) {
	var agent db.Agent
	var err error
	if version > 0 {
		agent, err = h.store.GetAgentVersion(ctx, workspaceUUID, agentID, version)
	} else {
		agent, err = h.store.GetAgent(ctx, workspaceUUID, agentID)
	}
	if err != nil {
		return db.Agent{}, mapStoreError(err, "Agent not found", loadFailedMessage)
	}
	if agent.ArchivedAt != nil {
		return db.Agent{}, invalidState(archivedAgentMessage, nil)
	}
	return agent, nil
}

func normalizeResources(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`[]`), nil
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, invalidArgument(resourcesInvalidMessage, err)
	}
	if _, ok := value.([]any); !ok {
		return nil, invalidArgument(resourcesInvalidMessage, nil)
	}
	return append(json.RawMessage(nil), trimmed...), nil
}

func normalizeVaults(values []string) ([]string, json.RawMessage, error) {
	if len(values) == 0 {
		return nil, json.RawMessage(`[]`), nil
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil, invalidArgument("vault_ids cannot include an empty id", nil)
		}
		cleaned = append(cleaned, value)
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return nil, nil, internalFailure(createFailedMessage, err)
	}
	return cleaned, raw, nil
}

func (h *Handler) finishStartup(ctx context.Context, principal auth.Principal, taskID string) (db.EvaluationTaskDetail, error) {
	detail, err := h.store.GetEvaluationTask(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
	if err != nil {
		return db.EvaluationTaskDetail{}, mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
	}
	status := "running"
	if allTerminalFailed(detail.Items) {
		status = "failed"
	}
	if err := h.store.UpdateEvaluationTaskStatus(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, taskID, status, "", time.Now().UTC()); err != nil {
		return db.EvaluationTaskDetail{}, internalFailure(createFailedMessage, err)
	}
	return h.store.GetEvaluationTask(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
}

func allTerminalFailed(items []db.EvaluationTaskItem) bool {
	if len(items) == 0 {
		return true
	}
	for _, item := range items {
		if item.Status != "failed" && item.Status != "cancelled" {
			return false
		}
	}
	return true
}

func (h *Handler) writeTask(w http.ResponseWriter, r *http.Request, principal auth.Principal, taskID string) error {
	detail, err := h.store.GetEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, taskID)
	if err != nil {
		return mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
	}
	return h.writeTaskDetail(w, r, principal, detail)
}
