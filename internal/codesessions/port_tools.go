package codesessions

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

type portURLRequest struct {
	Port int `json:"port"`
}

type portURLResponse struct {
	Type string `json:"type"`
	Port int    `json:"port"`
	URL  string `json:"url"`
}

func (h *Handler) WithSessionPorts(settings *sessionports.Settings) *Handler {
	h.sessionPorts = settings
	return h
}

func (h *Handler) handlePortURL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "code_session_id")
	claims, ok := h.authorizeSessionIngressClaims(w, r, id)
	if !ok {
		return
	}
	if err := h.db.ValidateCodeSessionIngressWorkerEpoch(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, id, claims.WorkerEpoch); err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	var input portURLRequest
	if !decodeWebInput(w, r, &input) {
		h.errorAdapter.Write(w, r, invalidSessionPort())
		return
	}
	address, err := h.sessionPortURL(r.Context(), claims, id, r.Header.Get(runtimeThreadHeader), input.Port)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, portURLResponse{Type: "session_port", Port: input.Port, URL: address})
}

func (h *Handler) sessionPortURL(ctx context.Context, claims SessionCredentialClaims, id, threadID string, port int) (string, error) {
	codeSession, found, err := h.db.GetCodeSession(ctx, id)
	if err != nil {
		return "", portToolContextFailure(err)
	}
	if !found || codeSession.OrganizationUUID != claims.OrganizationUUID || codeSession.WorkspaceUUID != claims.WorkspaceUUID {
		return "", portToolContextFailure(db.ErrNotFound)
	}
	session, found, err := h.db.GetSession(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return "", portToolContextFailure(err)
	}
	if !found || session.ArchivedAt != nil {
		return "", portToolContextFailure(db.ErrNotFound)
	}
	if err := h.requirePortTool(ctx, codeSession, session.AgentSnapshot, threadID); err != nil {
		return "", err
	}
	return sessionPortAddress(h.sessionPorts, session.UUID, port)
}

func sessionPortAddress(settings *sessionports.Settings, sessionUUID string, port int) (string, error) {
	if !settings.Enabled() {
		return "", sessionPortsDisabled()
	}
	if port < 1 || port > 65535 {
		return "", invalidSessionPort()
	}
	if !settings.PortAllowed(port) {
		return "", sessionPortBlocked(port)
	}
	address, err := settings.URL(sessionUUID, port)
	if err != nil {
		return "", internalError("Could not build session port address", err)
	}
	return address, nil
}

func (h *Handler) requirePortTool(ctx context.Context, codeSession db.CodeSession, rootSnapshot json.RawMessage, threadID string) error {
	snapshots := []json.RawMessage{rootSnapshot}
	if threadID != "" {
		chain, err := h.threadSnapshotChain(ctx, codeSession, threadID)
		if err != nil {
			return portToolContextFailure(err)
		}
		snapshots = append(snapshots, chain...)
	}
	for _, snapshot := range snapshots {
		enabled, err := builtinToolEnabled(snapshot, sessionports.ToolName)
		if err != nil {
			return portToolContextFailure(err)
		}
		if !enabled {
			return portToolDenied()
		}
	}
	return nil
}
