package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

const maxTraceText = 4000

type usageResponse struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
	JudgeInputTokens     int64 `json:"judge_input_tokens"`
	JudgeOutputTokens    int64 `json:"judge_output_tokens"`
}

type traceStepResponse struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type traceResponse struct {
	ItemID    string              `json:"item_id"`
	SessionID string              `json:"session_id"`
	Steps     []traceStepResponse `json:"steps"`
}

func (h *Handler) taskDetailJSON(ctx context.Context, principal auth.Principal, detail db.EvaluationTaskDetail) (taskResponse, error) {
	response := taskJSON(detail, true)
	task := detail.Task
	agent, err := h.store.GetAgentVersion(ctx, principal.WorkspaceUUID, task.AgentExternalID, task.AgentVersion)
	if err = ignoreMissing(err); err != nil {
		return taskResponse{}, err
	}
	response.AgentName = agent.Name
	dataset, err := h.store.GetEvaluationDataset(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, task.DatasetExternalID)
	if err = ignoreMissing(err); err != nil {
		return taskResponse{}, err
	}
	response.DatasetName = dataset.Name
	rubric, err := h.store.GetEvaluationRubric(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, task.RubricExternalID)
	if err = ignoreMissing(err); err != nil {
		return taskResponse{}, err
	}
	response.RubricName = rubric.Name
	usage, err := h.store.SumEvaluationTaskUsage(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, task.UUID)
	if err != nil {
		return taskResponse{}, internalFailure(loadFailedMessage, err)
	}
	response.Usage = &usageResponse{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadInputTokens: usage.CacheReadInputTokens,
		JudgeInputTokens: usage.JudgeInputTokens, JudgeOutputTokens: usage.JudgeOutputTokens,
	}
	return response, nil
}

func (h *Handler) getTaskItemTrace(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	detail, err := h.store.GetEvaluationTask(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "taskId"))
	if err != nil {
		return mapStoreError(err, taskNotFoundMessage, loadFailedMessage)
	}
	item, ok := taskItem(detail.Items, chi.URLParam(r, "itemId"))
	if !ok {
		return notFound(itemNotFoundMessage, nil)
	}
	steps, err := h.traceSteps(r.Context(), principal.WorkspaceUUID, item.SessionExternalID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, traceResponse{ItemID: item.ExternalID, SessionID: item.SessionExternalID, Steps: steps})
	return nil
}

func ignoreMissing(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return internalFailure(loadFailedMessage, err)
	}
	return nil
}

func taskItem(items []db.EvaluationTaskItem, externalID string) (db.EvaluationTaskItem, bool) {
	for _, item := range items {
		if item.ExternalID == externalID {
			return item, true
		}
	}
	return db.EvaluationTaskItem{}, false
}

func (h *Handler) traceSteps(ctx context.Context, workspaceUUID, sessionID string) ([]traceStepResponse, error) {
	steps := []traceStepResponse{}
	if sessionID == "" {
		return steps, nil
	}
	events, err := loadSessionEvents(ctx, h.store, workspaceUUID, sessionID)
	if err != nil {
		if errors.Is(err, errSessionTooLarge) {
			return nil, invalidArgument(sessionEventsTooLargeMessage, err)
		}
		return nil, internalFailure(loadFailedMessage, err)
	}
	for _, event := range events {
		step, ok := traceStep(event)
		if ok {
			steps = append(steps, step)
		}
	}
	return steps, nil
}

func traceStep(event db.SessionEvent) (traceStepResponse, bool) {
	step := traceStepResponse{CreatedAt: formatTime(event.CreatedAt)}
	switch event.EventType {
	case "user.message":
		step.Type, step.Text = "user", eventText(event.Payload)
	case "agent.message":
		step.Type, step.Text = "answer", eventText(event.Payload)
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		step.Type = "tool_use"
		step.Name, step.Text = toolUse(event.Payload)
	case "agent.tool_result", "agent.mcp_tool_result":
		step.Type, step.Text = "tool_result", eventText(event.Payload)
	case "session.error":
		step.Type, step.Text = "error", sessionErrorText(event.Payload)
	default:
		return traceStepResponse{}, false
	}
	step.Text = clip(step.Text, maxTraceText)
	return step, step.Text != "" || step.Name != ""
}

func toolUse(payload json.RawMessage) (string, string) {
	var body struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", ""
	}
	input := string(body.Input)
	if input == "null" || input == "{}" {
		input = ""
	}
	return body.Name, input
}

func sessionErrorText(payload json.RawMessage) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return body.Error.Message
}
