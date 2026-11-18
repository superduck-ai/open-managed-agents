package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

var errIdentityUnavailable = errors.New("identity binding is not configured")

func (h *Handler) WithIdentityGate(admit func(context.Context, string, string, string, bool) error, resolve func(context.Context, auth.Principal, string) (string, error)) *Handler {
	h.admitExecution = admit
	h.resolveIdentity = resolve
	return h
}

func (h *Handler) requestedSessionIdentity(ctx context.Context, principal auth.Principal, raw json.RawMessage) (string, error) {
	id, err := requestedIdentityID(raw)
	if err != nil || id == "" {
		return "", err
	}
	if h.resolveIdentity == nil {
		return "", internalError("Could not bind identity", errIdentityUnavailable)
	}
	return h.resolveIdentity(ctx, principal, id)
}

func eventBatchStartsRound(inputs []json.RawMessage) bool {
	for _, raw := range inputs {
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Type == "user.message" {
			return true
		}
	}
	return false
}

func requestedIdentityID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", invalidRequest(err)
	}
	id = strings.TrimSpace(id)
	if id == "" || !strings.HasPrefix(id, "idn_") {
		return "", invalidRequest(errors.New("identity_id is invalid"))
	}
	return id, nil
}
