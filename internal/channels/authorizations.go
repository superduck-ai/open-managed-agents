package channels

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

var authorizationIDPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func (h *handler) forwardAuthorization(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WorkspaceUUID == "" || principal.OrganizationUUID == "" {
		return unauthenticated()
	}
	if err := rejectBrowserCSRF(r, principal); err != nil {
		return err
	}
	endpoint, err := h.baseEndpoint()
	if err != nil {
		return err
	}
	endpoint.Path += "/authorizations"
	if id := chi.URLParam(r, "authorization_id"); id != "" {
		if !authorizationIDPattern.MatchString(id) {
			return invalidAuthorizationID()
		}
		endpoint = endpoint.JoinPath(id)
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint.String(), http.MaxBytesReader(w, r.Body, 8<<10))
	if err != nil {
		return unavailable(err)
	}
	request.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-OMA-Organization", principal.OrganizationUUID)
	request.Header.Set("X-OMA-Workspace", principal.WorkspaceUUID)
	response, err := h.client.Do(request)
	if err != nil {
		return unavailable(err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 || response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return unavailable(fmt.Errorf("授权桥接返回 HTTP %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<10))
	if err != nil {
		return unavailable(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, err = w.Write(body)
	return err
}
