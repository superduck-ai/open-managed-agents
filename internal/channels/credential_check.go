package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

var feishuTokenURL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"

var feishuAppIDPattern = regexp.MustCompile(`^cli_[A-Za-z0-9]+$`)

type appCredentialCheck struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

func (h *handler) checkAppCredentials(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WorkspaceUUID == "" || principal.OrganizationUUID == "" {
		return unauthenticated()
	}
	if err := rejectBrowserCSRF(r, principal); err != nil {
		return err
	}
	var input appCredentialCheck
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input); err != nil {
		return invalidCredentialCheckBody(err)
	}
	appID := strings.TrimSpace(input.AppID)
	if !feishuAppIDPattern.MatchString(appID) || input.AppSecret == "" {
		return invalidAppCredentials()
	}
	passed, err := h.feishuCredentialsMatch(r.Context(), appID, input.AppSecret)
	if err != nil {
		h.logger.WarnContext(r.Context(), "feishu app credential check failed", "workspace_id", principal.WorkspaceUUID, "error", err)
		return credentialCheckUnavailable(err)
	}
	if !passed {
		h.logger.InfoContext(r.Context(), "feishu app credential check rejected", "workspace_id", principal.WorkspaceUUID, "app_id", appID)
		return invalidAppCredentials()
	}
	h.logger.InfoContext(r.Context(), "feishu app credential check passed", "workspace_id", principal.WorkspaceUUID, "app_id", appID)
	httpapi.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (h *handler) feishuCredentialsMatch(ctx context.Context, appID, appSecret string) (bool, error) {
	endpoint := feishuTokenURL
	payload, err := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if err != nil {
		return false, err
	}
	if response.StatusCode >= 500 {
		return false, errFeishuStatus(response.StatusCode)
	}
	var result struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	return result.Code == 0, nil
}

type feishuStatusError int

func (status feishuStatusError) Error() string {
	return "feishu credential check failed"
}

func errFeishuStatus(status int) error {
	return feishuStatusError(status)
}
