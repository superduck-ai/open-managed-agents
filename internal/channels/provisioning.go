package channels

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

func (h *handler) syncWorkspaceCredentials(upstream *http.Request, principal auth.Principal) error {
	input := platform.CreateConsoleAPIKeyInput{OrgUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID}
	if principal.UserUUID != "" {
		input.CreatedByUserUUID = new(principal.UserUUID)
	}
	key, err := h.store.EnsureChannelWorkspaceCredential(upstream.Context(), input, h.cipher)
	if errors.Is(err, db.ErrChannelCredentialInactive) {
		return apperr.New(apperr.Conflict, "渠道专用 API Key 已停用，请在 API 密钥管理中恢复", err)
	}
	if err != nil {
		return apperr.New(apperr.Unavailable, "创建渠道工作区凭据失败，请联系管理员", err)
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"api_key"`
	}{APIKey: key})
	if err != nil {
		return unavailable(err)
	}
	endpoint, err := h.endpoint(upstream, true)
	if err != nil {
		return err
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/settings") + "/runtime"
	request := upstream.Clone(upstream.Context())
	request.URL = endpoint
	request.GetBody = nil
	request.ContentLength = int64(len(body))
	request.Body = io.NopCloser(bytes.NewReader(body))
	response, err := h.client.Do(request)
	if err != nil {
		return apperr.New(apperr.Unavailable, "同步渠道工作区凭据失败，请检查桥接服务", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return apperr.New(apperr.Unavailable, "同步渠道工作区凭据失败，请检查桥接版本与配置", fmt.Errorf("桥接运行配置同步返回 HTTP %d", response.StatusCode))
	}
	return nil
}
