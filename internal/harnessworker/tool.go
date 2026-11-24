package harnessworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"

	"charm.land/fantasy"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

type sandboxToolReply struct {
	State string `json:"state"`
}

func (w *Worker) loadContinuation(ctx context.Context, task db.HarnessTask) (modelContinuation, error) {
	tools, err := w.database.ListHarnessTools(ctx, task.Ref)
	if err != nil {
		return modelContinuation{}, err
	}
	return continuationFromTools(task.ToolUUID, tools)
}

func continuationFromTools(toolUUID string, tools []db.HarnessTool) (modelContinuation, error) {
	index := slices.IndexFunc(tools, func(tool db.HarnessTool) bool { return tool.UUID == toolUUID })
	if index < 0 || tools[index].Result == nil {
		return modelContinuation{}, errors.New("harness tool result is missing")
	}
	tool := tools[index]
	return modelContinuation{
		Result: cloudharness.ToolResult{CallID: cloudharness.ToolCallID(tool.CallID), Text: *tool.Result, Failed: tool.Failed},
		ToolCount: len(slices.DeleteFunc(slices.Clone(tools), func(other db.HarnessTool) bool {
			return delegationID(other) != delegationID(tool)
		})),
	}, nil
}

func (c modelContinuation) stepInput(sessionUUID, toolUUID string) cloudharness.StepInput {
	return cloudharness.StepInput{
		Message: cloudharness.InputMessage{GroupID: cloudharness.ThreadID(sessionUUID), Content: c.Result},
		DedupID: toolUUID + ":result",
	}
}

func delegationID(tool db.HarnessTool) string {
	if tool.DelegationToolUUID == nil {
		return ""
	}
	return *tool.DelegationToolUUID
}

func (w *Worker) modelTools(ctx context.Context, claim db.HarnessTurnClaim) ([]fantasy.Tool, error) {
	ref := claim.Lease.Ref
	session, found, err := w.database.GetSessionByUUID(ctx, ref.WorkspaceUUID, ref.SessionUUID)
	if err != nil {
		return nil, err
	}
	if !found || session.OrganizationUUID != ref.OrganizationUUID {
		return nil, db.ErrNotFound
	}
	allowed, err := codesessions.HarnessExecutionTools(session, claim)
	if err != nil {
		return nil, err
	}
	tools := make([]fantasy.Tool, 0, len(allowed))
	for _, tool := range allowed {
		definition := fantasy.FunctionTool{Name: tool.Name, Description: tool.Description}
		if err := json.Unmarshal(tool.InputSchema, &definition.InputSchema); err != nil {
			return nil, err
		}
		tools = append(tools, definition)
	}
	return tools, nil
}

func (w *Worker) executeTool(ctx context.Context, claim db.HarnessTurnClaim, token string) error {
	endpoint, err := url.JoinPath(w.apiURL, "v2", "ccr-sessions", claim.CodeSession.ExternalID, "harness", "turns", claim.Task.Ref.UUID, "tools", claim.Task.UUID)
	if err != nil {
		return err
	}
	client := llmproviders.NewHTTPClient(0)
	reply, err := requestSandboxTool(ctx, client, http.MethodPost, endpoint, token)
	if err == nil && !reply.done() {
		reply, err = requestSandboxTool(ctx, client, http.MethodGet, endpoint+"?wait_ms=1000", token)
	}
	if err != nil {
		return err
	}
	if !reply.done() {
		return w.database.ParkHarnessTool(ctx, claim.Lease, claim.Task.ToolUUID)
	}
	response, err := sendSandboxToolRequest(ctx, client, http.MethodPost, endpoint+"/result", token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("sandbox tool result gateway returned HTTP %d", response.StatusCode)
	}
	return nil
}

func sendSandboxToolRequest(ctx context.Context, client *http.Client, method, endpoint, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return client.Do(request)
}

func requestSandboxTool(ctx context.Context, client *http.Client, method, endpoint, token string) (sandboxToolReply, error) {
	response, err := sendSandboxToolRequest(ctx, client, method, endpoint, token)
	if err != nil {
		return sandboxToolReply{}, err
	}
	defer response.Body.Close()
	if method == http.MethodPost && response.StatusCode == http.StatusForbidden {
		return sandboxToolReply{State: "denied"}, nil
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return sandboxToolReply{}, fmt.Errorf("sandbox tool gateway returned HTTP %d", response.StatusCode)
	}
	var result sandboxToolReply
	err = json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&result)
	if err == nil && result.State == "" {
		err = errors.New("sandbox tool gateway returned no state")
	}
	return result, err
}

func (r sandboxToolReply) done() bool {
	switch r.State {
	case "succeeded", "failed", "cancelled", "expired", "lost", "denied":
		return true
	default:
		return false
	}
}
