package identities

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

func (s *Service) ListConfigs(ctx context.Context, principal auth.Principal, identityID, agentID, cursor string, limit int) ([]db.IdentityConfigRecord, string, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return nil, "", err
	}
	limit = pageLimit(limit)
	rows, err := s.store.ListIdentityConfigs(ctx, principal.WorkspaceUUID, identity.UUID, agentID, cursor, limit+1)
	if err != nil {
		return nil, "", unavailable(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].ExternalID
		rows = rows[:limit]
	}
	return rows, next, nil
}

func (s *Service) GetConfig(ctx context.Context, principal auth.Principal, identityID, agentID string) (db.IdentityConfigRecord, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return db.IdentityConfigRecord{}, err
	}
	row, err := s.store.FindActiveIdentityConfig(ctx, principal.WorkspaceUUID, identity.UUID, agentID)
	return row, mapStore(err, "身份配置不存在")
}

func (s *Service) PutConfig(ctx context.Context, principal auth.Principal, identityID, agentID string, expected int64, patch ConfigPatch) (db.IdentityConfigRecord, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return db.IdentityConfigRecord{}, err
	}
	for _, value := range patch.Env {
		if value != nil {
			return db.IdentityConfigRecord{}, invalid("执行身份暂不支持环境变量覆盖，请在原生执行环境中配置", nil)
		}
	}
	if _, err := s.visibleAgent(ctx, principal.WorkspaceUUID, agentID, 0); err != nil {
		return db.IdentityConfigRecord{}, err
	}
	current, err := s.store.FindActiveIdentityConfig(ctx, principal.WorkspaceUUID, identity.UUID, agentID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return db.IdentityConfigRecord{}, unavailable(err)
	}
	state := ConfigState{Env: map[string]string{}, Vaults: []string{}, Disabled: []string{}}
	revision := int64(0)
	if err == nil {
		state = configState(current)
		revision = current.Revision
		if expected != 0 && expected != revision {
			return db.IdentityConfigRecord{}, conflict("修订已变化，请读取最新内容后重试", nil)
		}
	}
	next, err := MergeConfig(state, patch)
	if err != nil {
		return db.IdentityConfigRecord{}, invalid("环境变量不能保存秘密或使用保留名称", err)
	}
	if err := s.authorizeVaults(ctx, principal.WorkspaceUUID, next.Vaults); err != nil {
		return db.IdentityConfigRecord{}, err
	}
	if !ConfigChanged(state, next) && current.ExternalID != "" {
		return current, nil
	}
	if current.ExternalID == "" {
		externalID, idErr := ids.New("idcfg_")
		if idErr != nil {
			return db.IdentityConfigRecord{}, unavailable(idErr)
		}
		created, createErr := s.store.CreateIdentityConfig(ctx, db.InsertIdentityConfigParams{
			UUID: uuid.NewString(), ExternalID: externalID, OrganizationUUID: principal.OrganizationUUID,
			WorkspaceUUID: principal.WorkspaceUUID, IdentityUUID: identity.UUID, AgentExternalID: agentID,
			Env: mustJSON(next.Env), VaultMounts: mustJSON(next.Vaults), DisabledVaults: mustJSON(next.Disabled),
		})
		return created, mapStore(createErr, "身份配置不存在")
	}
	if err := s.store.UpdateIdentityConfig(ctx, db.UpdateIdentityConfigParams{
		WorkspaceUUID: principal.WorkspaceUUID, ExternalID: current.ExternalID, ExpectedRevision: revision,
		Env: mustJSON(next.Env), VaultMounts: mustJSON(next.Vaults), DisabledVaults: mustJSON(next.Disabled),
	}); err != nil {
		return db.IdentityConfigRecord{}, mapStore(err, "身份配置不存在")
	}
	return s.store.FindActiveIdentityConfig(ctx, principal.WorkspaceUUID, identity.UUID, agentID)
}

func (s *Service) Effective(ctx context.Context, principal auth.Principal, identityID, agentID string, version int) (EffectiveConfig, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return EffectiveConfig{}, err
	}
	agent, err := s.visibleAgent(ctx, principal.WorkspaceUUID, agentID, version)
	if err != nil {
		return EffectiveConfig{}, err
	}
	cfg, err := s.store.FindActiveIdentityConfig(ctx, principal.WorkspaceUUID, identity.UUID, agentID)
	state := ConfigState{Env: map[string]string{}, Vaults: []string{}, Disabled: []string{}}
	revision := int64(0)
	if err == nil {
		state = configState(cfg)
		revision = cfg.Revision
	} else if !errors.Is(err, db.ErrNotFound) {
		return EffectiveConfig{}, unavailable(err)
	}
	base := AgentBase{Version: agent.CurrentVersion, Tools: toolNames(agent.Tools)}
	if version > 0 {
		base.Version = version
	}
	state.Env = nil
	return CompileEffective(base, state, revision, identity.AutoMemoryStore), nil
}

func (s *Service) visibleAgent(ctx context.Context, workspaceUUID, agentID string, version int) (db.Agent, error) {
	var agent db.Agent
	var err error
	if version > 0 {
		agent, err = s.store.GetAgentVersion(ctx, workspaceUUID, agentID, version)
	} else {
		agent, err = s.store.GetAgent(ctx, workspaceUUID, agentID)
	}
	if errors.Is(err, db.ErrNotFound) {
		return db.Agent{}, notFound("Agent 不存在或版本不可用", err)
	}
	if err != nil {
		return db.Agent{}, unavailable(err)
	}
	if agent.ArchivedAt != nil {
		return db.Agent{}, conflict("Agent 已归档", nil)
	}
	return agent, nil
}

func (s *Service) authorizeVaults(ctx context.Context, workspaceUUID string, vaultIDs []string) error {
	for _, id := range vaultIDs {
		vault, err := s.store.GetVault(ctx, workspaceUUID, id)
		if errors.Is(err, db.ErrNotFound) || vault.ArchivedAt != nil || vault.DeletedAt != nil {
			return invalid("共享保险库不存在或已停用", err)
		}
		if err != nil {
			return unavailable(err)
		}
		personal, err := s.store.IsPersonalChannelVault(ctx, workspaceUUID, id)
		if err != nil {
			return unavailable(err)
		}
		if personal {
			return conflict("不能把其他操作者的个人保险库挂成共享凭据", nil)
		}
	}
	return nil
}

func configState(row db.IdentityConfigRecord) ConfigState {
	return ConfigState{Env: decodeObject(row.Env), Vaults: decodeList(row.VaultMounts), Disabled: decodeList(row.DisabledVaults)}
}

func configView(row db.IdentityConfigRecord) map[string]any {
	state := configState(row)
	return map[string]any{
		"id": row.ExternalID, "agent_id": row.AgentExternalID, "revision": row.Revision, "status": row.Status,
		"env": state.Env, "vault_mounts": state.Vaults, "disabled_vaults": state.Disabled,
		"updated_at": row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toolNames(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var names []string
	if json.Unmarshal(raw, &names) == nil {
		return names
	}
	var objects []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &objects) != nil {
		return nil
	}
	for _, object := range objects {
		if object.Name != "" {
			names = append(names, object.Name)
		} else if object.Type != "" {
			names = append(names, object.Type)
		}
	}
	return names
}
