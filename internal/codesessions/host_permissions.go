package codesessions

import (
	"context"
	"encoding/json"
)

type HostToolRequest struct {
	RequestID string
	ToolUseID string
	ToolName  string
	ThreadID  string
	Input     json.RawMessage
}

type HostToolPermission struct {
	RequestID     string
	PublicEventID string
	Behavior      string
}

func hostPermissionPayload(request HostToolRequest) (workerControlRequestPayload, error) {
	if request.RequestID == "" || request.ToolUseID == "" || request.ToolName == "" {
		return workerControlRequestPayload{}, ErrHostPermissionInvalid
	}
	var input map[string]any
	if err := json.Unmarshal(request.Input, &input); err != nil || input == nil {
		return workerControlRequestPayload{}, ErrHostPermissionInvalid
	}
	return workerControlRequestPayload{
		workerPayloadHeader: workerPayloadHeader{Type: "control_request", UUID: request.RequestID}, RequestID: request.RequestID, SessionThreadID: request.ThreadID,
		Request: workerPermissionRequest{Subtype: "can_use_tool", ToolName: request.ToolName, ToolUseID: request.ToolUseID, Input: input, SessionThreadID: request.ThreadID},
	}, nil
}

func (w *HostWorker) RequestPermission(ctx context.Context, input HostToolRequest) (HostToolPermission, error) {
	if err := w.service.db.ValidateCodeSessionWorkerEpoch(ctx, w.record.ExternalID, w.epoch); err != nil {
		return HostToolPermission{}, err
	}
	payload, err := hostPermissionPayload(input)
	if err != nil {
		return HostToolPermission{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return HostToolPermission{}, err
	}
	metadata, err := BuildEventMetadata(w.record.ExternalID, "outbound", raw)
	if err != nil {
		return HostToolPermission{}, err
	}
	permission, identity, err := w.service.resolveToolPermission(ctx, w.record.ExternalID, input.ToolName)
	if err != nil {
		return HostToolPermission{}, err
	}
	request, events, err := toolPermissionPublicPayloads(w.record.ExternalID, &payload, metadata, identity, permission)
	if err != nil {
		return HostToolPermission{}, err
	}
	if len(events) == 0 {
		return HostToolPermission{}, ErrHostPermissionInvalid
	}
	if permission == resolvedToolPermissionAsk {
		if err := w.service.persistToolPermissionRequest(ctx, w.record.ExternalID, w.epoch, request); err != nil {
			return HostToolPermission{}, err
		}
	}
	if err := w.service.publishPublicPayloads(ctx, w.record.ExternalID, events); err != nil {
		return HostToolPermission{}, err
	}
	if permission != resolvedToolPermissionAsk {
		if err := w.service.respondToToolPermissionRequest(ctx, w.record.ExternalID, w.epoch, request, permission, "host-policy", "", "", ""); err != nil {
			return HostToolPermission{}, err
		}
	}
	return HostToolPermission{RequestID: request.RequestID, PublicEventID: request.PublicEventID, Behavior: string(permission)}, nil
}
