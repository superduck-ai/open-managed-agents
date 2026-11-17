package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var errIdentityUnavailable = errors.New("identity binding is not configured")

func (h *Handler) WithIdentityGate(admit func(context.Context, string, string, string, bool) error, prepare func(context.Context, auth.Principal, string) error, bind func(context.Context, auth.Principal, db.Session, string) error) *Handler {
	h.admitExecution = admit
	h.prepareIdentity = prepare
	h.bindIdentity = bind
	return h
}

func (h *Handler) requestedSessionIdentity(ctx context.Context, principal auth.Principal, raw json.RawMessage) (string, error) {
	id, err := requestedIdentityID(raw)
	if err != nil {
		return "", err
	}
	if id != "" && h.prepareIdentity != nil {
		if err := h.prepareIdentity(ctx, principal, id); err != nil {
			return "", err
		}
	}
	return id, nil
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
