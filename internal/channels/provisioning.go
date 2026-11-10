package channels

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/platformapi"
)

func (h *handler) syncWorkspaceCredentials(upstream *http.Request, principal auth.Principal) error {
	createdByUserUUID, err := platformapi.ResolveConsoleAPIKeyCreator(upstream.Context(), h.store, principal)
	if err != nil {
		return credentialProvisionFailed(err)
	}
	input := platform.CreateConsoleAPIKeyInput{OrgUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID, CreatedByUserUUID: createdByUserUUID, RuntimeUserUUID: principal.RuntimeUserUUID()}
	key, err := h.store.EnsureChannelWorkspaceCredential(upstream.Context(), input, h.cipher)
	if errors.Is(err, db.ErrChannelCredentialInactive) {
		return credentialInactive(err)
	}
	if errors.Is(err, db.ErrChannelCredentialUnreadable) {
		return credentialUnreadable(err)
	}
	if errors.Is(err, db.ErrNotFound) {
		return credentialWorkspaceNotFound(err)
	}
	if err != nil {
		return credentialProvisionFailed(err)
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"api_key"`
	}{APIKey: key})
	if err != nil {
		return unavailable(err)
	}
	endpoint, err := h.baseEndpoint()
	if err != nil {
		return err
	}
	endpoint.Path += "/internal/workspace-runtime"
	request := upstream.Clone(upstream.Context())
	request.URL = endpoint
	request.GetBody = nil
	request.ContentLength = int64(len(body))
	request.Body = io.NopCloser(bytes.NewReader(body))
	response, err := h.client.Do(request)
	if err != nil {
		return credentialSyncUnavailable(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return credentialSyncRejected(fmt.Errorf("桥接运行配置同步返回 HTTP %d", response.StatusCode))
	}
	return nil
}
