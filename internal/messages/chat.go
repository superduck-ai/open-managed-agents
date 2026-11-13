package messages

import (
	"encoding/json"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.openAI(w, r, llmproviders.ChatCompletions)
}

func (h *Handler) openAI(w http.ResponseWriter, r *http.Request, backend string) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, brainRequiredError())
		return
	}
	if principal.CodeSessionExternalID == "" {
		h.create(w, r, backend)
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
	h.create(w, r, backend)
}

func validateChatRequest(body []byte) error {
	var request struct {
		Stream *bool `json:"stream"`
		N      *int  `json:"n"`
	}
	if json.Unmarshal(body, &request) != nil {
		return errChatChoiceCount
	}
	if request.N != nil && *request.N != 1 {
		return errChatChoiceCount
	}
	return nil
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	h.openAI(w, r, llmproviders.Responses)
}
