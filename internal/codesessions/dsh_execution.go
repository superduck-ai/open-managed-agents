package codesessions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
)

type dshExecution struct{ handler *Handler }

type dshCommittedSink interface {
	PublishCommittedCodeSessionEvents(context.Context, db.CodeSession, db.SessionEventChanges) error
}

func (e dshExecution) committed(ctx context.Context, scope db.DSHScope, changes db.SessionEventChanges) error {
	if len(changes.Events) == 0 {
		return nil
	}
	sink, ok := e.handler.service.sink.(dshCommittedSink)
	if !ok {
		return nil
	}
	return sink.PublishCommittedCodeSessionEvents(ctx, db.CodeSession{ExternalID: scope.CodeSessionID, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID}, changes)
}

func (e dshExecution) Stream(w http.ResponseWriter, r *http.Request, _ db.DSHScope) {
	e.handler.handleCodeSessionWorkerEventsStream(w, r)
}

func (e dshExecution) Delivery(ctx context.Context, scope db.DSHScope, updates []dsh.DeliveryUpdate) (dsh.DeliveryResponse, error) {
	ids := make([]string, len(updates))
	for i, update := range updates {
		ids[i] = update.ID
	}
	if err := e.handler.db.ValidateDSHControlInputs(ctx, scope, ids); err != nil {
		return dsh.DeliveryResponse{}, err
	}
	response := dsh.DeliveryResponse{Results: make([]dsh.DeliveryItem, 0, len(updates))}
	for _, update := range updates {
		outcome, err := e.controlDelivery(ctx, scope, update)
		if err != nil {
			return dsh.DeliveryResponse{}, err
		}
		response.Results = append(response.Results, dsh.DeliveryItem{ID: update.ID, Status: update.Status, Outcome: outcome})
		if outcome == "applied" {
			response.Applied++
		} else {
			response.Ignored++
		}
	}
	return response, nil
}

func (e dshExecution) Claim(ctx context.Context, scope db.DSHScope) (dsh.InputClaim, error) {
	claim, changes, err := e.handler.db.ClaimDSHInput(ctx, scope)
	if err != nil {
		return dsh.InputClaim{}, err
	}
	if err := e.committed(ctx, scope, changes); err != nil {
		return dsh.InputClaim{}, err
	}
	if claim.Event.ExternalID == "" {
		return dsh.InputClaim{}, nil
	}
	event, err := e.handler.service.eventPayloads.RestorePublic(ctx, claim.Event)
	if err != nil {
		return dsh.InputClaim{}, err
	}
	code := db.CodeSession{ExternalID: scope.CodeSessionID, UUID: scope.CodeSessionUUID, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID}
	payload, err := e.handler.service.dshMessagePayload(ctx, code, event)
	return dsh.InputClaim{Input: payload, CancelRequestedAt: claim.CancelRequestedAt}, err
}

func (e dshExecution) controlDelivery(ctx context.Context, scope db.DSHScope, result dsh.DeliveryUpdate) (string, error) {
	service := e.handler.service
	refs, err := service.workerEventAcks.GetMany(ctx, scope.CodeSessionID, scope.Epoch, []string{result.ID})
	if err != nil {
		return "", err
	}
	ref, found := refs[result.ID]
	if !found {
		return "ignored", nil
	}
	if result.Status != "processed" {
		if err := service.workerEvents.InProgress(ctx, ref.AckSubject); err != nil {
			return "", err
		}
		return "applied", service.workerEventAcks.Refresh(ctx, scope.CodeSessionID, scope.Epoch, result.ID)
	}
	if err := service.workerEvents.DoubleAck(ctx, ref.AckSubject); err != nil {
		return "", err
	}
	if err := service.workerEventAcks.Delete(ctx, scope.CodeSessionID, scope.Epoch, result.ID); err != nil {
		service.logger.WarnContext(ctx, "delete DSR control delivery", "code_session_id", scope.CodeSessionID, "error", err)
	}
	service.triggerPayloadCleanupNow(ctx, ref.CleanupJobID)
	return "applied", nil
}

func (e dshExecution) Finish(ctx context.Context, scope db.DSHScope, id string, input dsh.FinishRequest) (dsh.FinishRequest, error) {
	result, changes, err := e.handler.db.FinishDSHInput(ctx, scope, id, db.DSHFinish{Result: input.Result, FailureCode: input.FailureCode})
	if err != nil {
		return dsh.FinishRequest{}, err
	}
	return dsh.FinishRequest{Result: result.Result, FailureCode: result.FailureCode}, e.committed(ctx, scope, changes)
}

func (e dshExecution) Permission(ctx context.Context, scope db.DSHScope, input dsh.PermissionRequest) (dsh.PermissionResponse, error) {
	id := dsh.PermissionID(scope.CodeSessionID, input.DSHSessionID, input.ToolUseID)
	tool, _ := json.Marshal(input.Tool)
	stored, found, err := e.handler.db.ReadDSHPermission(ctx, scope, db.DSHPermission{RequestID: id, InputID: input.InputID, DSHSessionID: input.DSHSessionID, ToolUseID: input.ToolUseID, Turn: input.Turn, Tool: tool, Input: input.Input, Available: input.Available})
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	if found {
		return permissionResponse(stored), nil
	}

	threadID, err := e.permissionThread(ctx, scope, input.DSHSessionID)
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	name := input.Tool.Name
	if input.Tool.Kind == "mcp" {
		name = "mcp__" + input.Tool.Server + "__" + name
	}
	permission, identity, err := e.handler.service.resolveToolPermission(ctx, scope.CodeSessionID, name, threadID)
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	if input.Available != nil && !*input.Available {
		permission = resolvedToolPermissionDeny
	}
	if input.Tool.Kind == "mcp" && (identity.Kind != "mcp" || identity.ServerName != input.Tool.Server || identity.ToolName != input.Tool.Name) {
		return dsh.PermissionResponse{}, db.ErrInvalidState
	}
	rawTool, _ := json.Marshal(input.Tool)
	request := db.DSHPermission{RequestID: id, InputID: input.InputID, DSHSessionID: input.DSHSessionID, ThreadID: threadID, ToolUseID: input.ToolUseID, Turn: input.Turn, Tool: rawTool, Input: input.Input, Available: input.Available, State: "pending"}
	outputThread := threadID
	if input.DSHSessionID == scope.CodeSessionID {
		outputThread = ""
	}
	public, raw, err := dshToolCallPublicPayload(scope.CodeSessionID, toolPermissionRequest{RequestID: id, ToolUseID: input.ToolUseID, ToolName: name, SessionThreadID: outputThread}, input.Input, identity, permission, time.Now().UTC())
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	request.PublicEventID = public.PublicEventID
	external, err := e.handler.service.selfHostedCodeSession(ctx, scope.CodeSessionID)
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	waiting := input.Tool.Kind == "custom" || public.EventType == "agent.custom_tool_use" || external && identity.Kind == "agent_toolset"
	if permission == resolvedToolPermissionDeny || permission == resolvedToolPermissionAllow && !waiting {
		request.State = "resolved"
		request.Decision, _ = json.Marshal(struct {
			Behavior string `json:"behavior"`
		}{string(permission)})
	}
	events, err := dshPermissionEvents(scope, public, raw)
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	result, changes, err := e.handler.db.RequestDSHPermission(ctx, scope, request, events)
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	return permissionResponse(result), e.committed(ctx, scope, changes)
}

func (e dshExecution) permissionThread(ctx context.Context, scope db.DSHScope, id string) (string, error) {
	if id != scope.CodeSessionID {
		row, _, err := e.handler.db.ReadDSHTranscript(ctx, scope, id, 0, 1)
		if err != nil {
			return "", err
		}
		if row.SessionThreadID == nil {
			return "", db.ErrDSHInputNotEligible
		}
		return *row.SessionThreadID, nil
	}
	primary, found, err := e.handler.db.GetPrimarySessionThreadExternalID(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", db.ErrNotFound
	}
	return primary, nil
}

func permissionResponse(value db.DSHPermission) dsh.PermissionResponse {
	return dsh.PermissionResponse{RequestID: value.RequestID, State: value.State, Decision: value.Decision}
}

func (e dshExecution) CancelPermission(ctx context.Context, scope db.DSHScope, id string, input dsh.PermissionCancel) (dsh.PermissionResponse, error) {
	result, changes, err := e.handler.db.CancelDSHPermission(ctx, scope, db.DSHPermission{RequestID: id, DSHSessionID: input.DSHSessionID, ToolUseID: input.ToolUseID, InputID: input.InputID})
	if err != nil {
		return dsh.PermissionResponse{}, err
	}
	return permissionResponse(result), e.committed(ctx, scope, changes)
}

func (e dshExecution) Publish(ctx context.Context, scope db.DSHScope, raws []json.RawMessage) (dsh.PublishResponse, error) {
	batch, previews, err := prepareDSHPublicEvents(scope, raws)
	if err != nil {
		return dsh.PublishResponse{}, err
	}
	if err := e.validatePublicThreads(ctx, scope, batch); err != nil {
		return dsh.PublishResponse{}, err
	}
	result, err := publishDSHBatch(ctx, e.handler.db, scope, batch)
	if err != nil {
		return dsh.PublishResponse{}, err
	}
	if err := e.committed(ctx, scope, result.Changes); err != nil {
		return dsh.PublishResponse{}, err
	}
	if sink, ok := e.handler.service.sink.(interface {
		PublishDSHPreviews(context.Context, db.DSHScope, []json.RawMessage) error
	}); ok {
		if err := sink.PublishDSHPreviews(ctx, scope, previews); err != nil {
			e.handler.logger.WarnContext(ctx, "publish DSR preview", "code_session_id", scope.CodeSessionID, "error", err)
		}
	}
	return dsh.PublishResponse{Accepted: result.Accepted, Duplicates: result.Duplicates}, nil
}

func writeDSHInput(w io.Writer, flusher http.Flusher, event db.CodeSessionEvent) error {
	if _, err := w.Write([]byte("event: input\ndata: " + string(event.Payload) + "\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
