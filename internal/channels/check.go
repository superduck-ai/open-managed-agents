package channels

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type channelCheckItem struct {
	Code    string `json:"code"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type channelCheckResult struct {
	OK                bool               `json:"ok"`
	ExecutionVerified bool               `json:"execution_verified"`
	Checks            []channelCheckItem `json:"checks"`
}

type channelCheckTarget struct {
	code, id, path, message string
}

type channelCheckResource struct {
	ID         string     `json:"id"`
	ArchivedAt *time.Time `json:"archived_at"`
}

func (h *localHandler) check(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, false)
	if err != nil {
		return err
	}
	record, err := h.service.store.GetChannelRecord(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "agent_id"), chi.URLParam(r, "channel_id"))
	if errors.Is(err, db.ErrNotFound) {
		return channelNotFound()
	}
	if err != nil {
		return err
	}
	if h.service.runtime == nil {
		return unavailable(nil)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	httpapi.WriteJSON(w, http.StatusOK, h.service.runtime.check(ctx, record))
	return nil
}

func (s *runtimeSupervisor) check(ctx context.Context, record db.ChannelRecord) channelCheckResult {
	result := channelCheckResult{OK: true, Checks: make([]channelCheckItem, 0, 7)}
	add := func(code string, ok bool, message string) {
		if ok {
			message = ""
		}
		result.Checks = append(result.Checks, channelCheckItem{Code: code, OK: ok, Message: message})
		result.OK = result.OK && ok
	}
	add("enabled", record.Enabled, "渠道已停用")
	add("binding", record.BindingStatus == "bound", "尚未绑定渠道应用")
	add("connection", s.online(record.ExternalID), "渠道连接未建立")
	key, err := s.store.EnsureChannelWorkspaceCredential(ctx, platform.CreateConsoleAPIKeyInput{OrgUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID}, s.cipher)
	if err != nil {
		message := "渠道运行凭据不可用"
		if public, ok := errors.AsType[*apperr.Error](channelCredentialError(err)); ok {
			message = public.PublicMessage
		}
		add("credential", false, message)
		return result
	}
	add("credential", true, "")
	environmentID, err := s.store.GetChannelAgentEnvironment(ctx, record.OrganizationUUID, record.WorkspaceUUID, record.AgentExternalID)
	add("environment_binding", err == nil && environmentID != "", "尚未配置可用执行环境")
	mounts := parseChannelMounts(record.Mounts)
	resources := []channelCheckTarget{
		{"agent", record.AgentExternalID, "agents", "渠道运行凭据无法访问智能体，或智能体已归档"},
	}
	if err == nil && environmentID != "" {
		resources = append(resources, channelCheckTarget{"environment", environmentID, "environments", "渠道运行凭据无法访问执行环境，或环境已归档"})
	}
	if mounts.MemoryStoreID != "" {
		resources = append(resources, channelCheckTarget{"memory", mounts.MemoryStoreID, "memory_stores", "挂载的Memory不可用或已归档"})
	}
	for _, vaultID := range mounts.VaultIDs {
		resources = append(resources, channelCheckTarget{"vault", vaultID, "vaults", "挂载的Vault不可用或已归档"})
	}
	configured, resourceErr := configuredChannelResources(record)
	add("resources", resourceErr == nil, "渠道资源配置不可用")
	for _, resource := range configured {
		if resource.Type != "file" {
			continue
		}
		file, err := s.oma.fileMetadata(ctx, key, resource.FileID)
		add("file", err == nil && file.ID == resource.FileID, "配置文件不可用或渠道运行凭据无权访问")
	}
	for _, resource := range resources {
		var found channelCheckResource
		path := "/v1/" + resource.path + "/" + url.PathEscape(resource.id)
		if resource.code == "agent" && mounts.AgentVersion > 0 {
			path += "?version=" + strconv.Itoa(mounts.AgentVersion)
		}
		err := s.oma.request(ctx, key, http.MethodGet, path, nil, &found)
		add(resource.code, err == nil && found.ID == resource.id && found.ArchivedAt == nil, resource.message)
	}
	return result
}
