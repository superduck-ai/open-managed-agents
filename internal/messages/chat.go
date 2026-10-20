package messages

import (
	"encoding/json"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.CodeSessionExternalID == "" {
		httpapi.WriteError(w, r, brainRequiredError())
		return
	}
	transport, err := h.database.GetCodeSessionWorkerTransport(r.Context(), principal.CodeSessionExternalID)
	if err != nil {
		httpapi.WriteError(w, r, chatSessionLookupError(err))
		return
	}
	if transport != "claim" {
		httpapi.WriteError(w, r, brainRequiredError())
		return
	}
	h.create(w, r, llmproviders.ChatCompletions)
}

func validateChatRequest(body []byte) error {
	var request struct {
		Stream bool `json:"stream"`
		N      *int `json:"n"`
	}
	if json.Unmarshal(body, &request) != nil {
		return errChatStreamRequired
	}
	if !request.Stream || (request.N != nil && *request.N != 1) {
		return errChatStreamRequired
	}
	return nil
}
