package identities

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Ingress struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	ChannelUUID       string
	ChannelExternalID string
	ChannelType       string
	AppID             string
	AgentExternalID   string
	ChatType          string
	RemoteID          string
	ActorID           string
	ScopeKey          string
	ScopeKind         string
	DisplayName       string
	PersonalVaultID   string
	PersonalActive    bool
}

type Decision struct {
	Mode               string
	Notice             string
	AgentExternalID    string
	AgentVersion       int
	IdentityUUID       string
	IdentityExternalID string
	BindingRevision    int64
	ConfigRevision     int64
	VaultIDs           []string
	PersonalVaultID    string
	Env                map[string]string
	ScopeKind          string
	ScopeKey           string
}

type ChannelContextInput struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	ChannelUUID       string
	ChannelExternalID string
	Mode              string
	IdentityID        string
	AgentID           string
	AgentVersion      *int
}

type SessionBind struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionUUID       string
	SessionExternalID string
	IdentityUUID      string
	AgentExternalID   string
	BindingRevision   int64
	ConfigRevision    int64
	PersonalVaultID   string
	ScopeKind         string
	ScopeKey          string
	ChannelExternalID string
}

func (s *Service) Resolve(ctx context.Context, ingress Ingress) (Decision, error) {
	return s.resolve(ctx, ingress, true)
}

func (s *Service) Preview(ctx context.Context, ingress Ingress) (Decision, error) {
	return s.resolve(ctx, ingress, false)
}

func (s *Service) resolve(ctx context.Context, ingress Ingress, create bool) (Decision, error) {
	execution, err := s.store.FindExecutionContext(ctx, ingress.WorkspaceUUID, ingress.ChannelExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return Decision{Mode: "legacy"}, nil
	}
	if err != nil {
		return Decision{}, err
	}
	if execution.EntryClosed {
		return Decision{Mode: "hold", Notice: "该渠道的执行入口已关闭，本条消息未执行。"}, nil
	}
	if execution.Mode == "fixed" {
		return s.resolveFixed(ctx, ingress, execution)
	}
	return s.resolvePaired(ctx, ingress, create)
}

func (s *Service) resolveFixed(ctx context.Context, ingress Ingress, execution db.ExecutionContextRecord) (Decision, error) {
	if execution.IdentityUUID == nil {
		return Decision{Mode: "hold", Notice: "固定执行身份尚未配置，本条消息未执行。"}, nil
	}
	identity, err := s.store.FindBusinessIdentityByUUID(ctx, ingress.WorkspaceUUID, *execution.IdentityUUID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && !identity.Enabled) {
		return Decision{Mode: "hold", Notice: "业务身份已停用，新的执行已停止。进行中的任务会继续收尾。"}, nil
	}
	if err != nil {
		return Decision{}, err
	}
	agentID := ingress.AgentExternalID
	version := 0
	if execution.AgentVersion != nil {
		version = *execution.AgentVersion
		if _, versionErr := s.store.GetAgentVersion(ctx, ingress.WorkspaceUUID, agentID, version); versionErr != nil {
			return Decision{Mode: "hold", Notice: "固定的智能体版本不可用，未回退到最新版本，本条消息未执行。"}, nil
		}
	}
	ingress.ScopeKind = ScopeKind(ingress.ChatType)
	return s.finishDecision(ctx, ingress, identity, agentID, version, execution.Revision, ingress.ScopeKind)
}

func (s *Service) resolvePaired(ctx context.Context, ingress Ingress, create bool) (Decision, error) {
	ingress.ScopeKind = ScopeKind(ingress.ChatType)
	if ingress.ChatType == "p2p" {
		ingress.RemoteID = ingress.ActorID
	}
	pairing, code, created, err := s.openPairing(ctx, ingress, create)
	if err != nil {
		return Decision{}, err
	}
	if pairing.Status != "active" || pairing.IdentityUUID == nil {
		notice := "该范围尚未绑定执行身份。配对编号 " + pairing.ExternalID + "。请管理员绑定后重新发送，本条消息不会执行。"
		if created {
			notice = "该范围尚未绑定执行身份。配对编号 " + pairing.ExternalID + "，配对码 " + code + "，10 分钟内有效。请管理员绑定后重新发送，本条消息不会执行。"
		}
		return Decision{Mode: "hold", Notice: notice}, nil
	}
	identity, err := s.store.FindBusinessIdentityByUUID(ctx, ingress.WorkspaceUUID, *pairing.IdentityUUID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && !identity.Enabled) {
		return Decision{Mode: "hold", Notice: "业务身份已停用，新的执行已停止。进行中的任务会继续收尾。"}, nil
	}
	if err != nil {
		return Decision{}, err
	}
	version := 0
	if pairing.AgentVersion != nil {
		version = *pairing.AgentVersion
		if _, versionErr := s.store.GetAgentVersion(ctx, ingress.WorkspaceUUID, pairing.AgentExternalID, version); versionErr != nil {
			return Decision{Mode: "hold", Notice: "配对的智能体版本不可用，未回退到最新版本，本条消息未执行。"}, nil
		}
	}
	decision, err := s.finishDecision(ctx, ingress, identity, pairing.AgentExternalID, version, pairing.Revision, ingress.ScopeKind)
	return decision, err
}

func (s *Service) finishDecision(ctx context.Context, ingress Ingress, identity db.IdentityRecord, agentID string, version int, bindingRevision int64, scopeKind string) (Decision, error) {
	state := ConfigState{}
	revision := int64(0)
	cfg, err := s.store.FindActiveIdentityConfig(ctx, ingress.WorkspaceUUID, identity.UUID, agentID)
	if err == nil {
		state = configState(cfg)
		revision = cfg.Revision
	} else if !errors.Is(err, db.ErrNotFound) {
		return Decision{}, err
	}
	mounts := EnabledMounts(state.Vaults, state.Disabled)
	availability := map[string]VaultState{}
	for _, id := range mounts {
		availability[id] = s.vaultState(ctx, ingress.WorkspaceUUID, id)
	}
	credentials, err := PlanCredentials(CredentialInput{
		ChatType: ingress.ChatType, SharedMounts: mounts, Availability: availability,
		PersonalVault: ingress.PersonalVaultID, PersonalActive: ingress.PersonalActive,
	})
	if err != nil {
		return Decision{Mode: "hold", Notice: "共享凭据不可用，未改用个人凭据，本条消息未执行。"}, nil
	}
	return Decision{
		Mode: "execute", AgentExternalID: agentID, AgentVersion: version, IdentityUUID: identity.UUID,
		IdentityExternalID: identity.ExternalID, BindingRevision: bindingRevision, ConfigRevision: revision,
		VaultIDs: credentials.VaultIDs, PersonalVaultID: credentials.PersonalVaultID, Env: state.Env,
		ScopeKind: scopeKind, ScopeKey: ingress.ScopeKey,
	}, nil
}

func (s *Service) vaultState(ctx context.Context, workspaceUUID, vaultID string) VaultState {
	vault, err := s.store.GetVault(ctx, workspaceUUID, vaultID)
	if errors.Is(err, db.ErrNotFound) || vault.DeletedAt != nil {
		return VaultMissing
	}
	if err != nil || vault.ArchivedAt != nil {
		return VaultArchived
	}
	return VaultActive
}

func (s *Service) SaveChannelContext(ctx context.Context, principal auth.Principal, input ChannelContextInput) error {
	if input.Mode != "fixed" && input.Mode != "pairing" {
		return invalid("身份解析模式只接受 fixed 或 pairing", nil)
	}
	if input.Mode == "fixed" && (input.IdentityID == "" || input.AgentID == "") {
		return invalid("固定上下文需要执行身份和智能体", nil)
	}
	if input.Mode == "pairing" && input.IdentityID != "" {
		return invalid("范围配对创建时不能指定执行身份", nil)
	}
	var identityUUID *string
	if input.IdentityID != "" {
		identity, err := s.GetIdentity(ctx, principal, input.IdentityID)
		if err != nil {
			return err
		}
		identityUUID = &identity.UUID
	}
	if input.AgentID != "" {
		if _, err := s.visibleAgent(ctx, principal.WorkspaceUUID, input.AgentID, versionValue(input.AgentVersion)); err != nil {
			return err
		}
	}
	current, err := s.store.FindExecutionContext(ctx, input.WorkspaceUUID, input.ChannelExternalID)
	if errors.Is(err, db.ErrNotFound) {
		_, createErr := s.store.CreateExecutionContext(ctx, db.InsertExecutionContextParams{
			UUID: uuid.NewString(), OrganizationUUID: input.OrganizationUUID, WorkspaceUUID: input.WorkspaceUUID,
			ChannelUUID: input.ChannelUUID, ChannelExternalID: input.ChannelExternalID, Mode: input.Mode,
			IdentityUUID: identityUUID, AgentVersion: input.AgentVersion,
		})
		return mapStore(createErr, "渠道不存在")
	}
	if err != nil {
		return unavailable(err)
	}
	if current.Mode != input.Mode {
		return conflict("身份解析模式创建后不能修改", nil)
	}
	if sameExecution(current, identityUUID, input.AgentVersion) {
		return nil
	}
	return mapStore(s.store.UpdateExecutionTarget(ctx, db.UpdateExecutionTargetParams{
		WorkspaceUUID: input.WorkspaceUUID, ChannelUUID: input.ChannelUUID, Mode: current.Mode,
		IdentityUUID: identityUUID, AgentVersion: input.AgentVersion, Revision: current.Revision,
	}), "渠道不存在")
}

func (s *Service) ObserveConnection(ctx context.Context, workspaceUUID, channelExternalID, status string) error {
	if status != "healthy" && status != "disconnected" && status != "unknown" {
		return nil
	}
	err := s.store.SetChannelConnectionStatus(ctx, workspaceUUID, channelExternalID, status)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) BindSession(ctx context.Context, bind SessionBind) error {
	if bind.IdentityUUID == "" || bind.SessionExternalID == "" {
		return nil
	}
	if bind.SessionUUID == "" {
		session, found, err := s.store.GetSession(ctx, bind.WorkspaceUUID, bind.SessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return db.ErrNotFound
		}
		bind.SessionUUID = session.UUID
	}
	return s.store.BindSessionIdentity(ctx, db.InsertSessionIdentityParams{
		UUID: uuid.NewString(), OrganizationUUID: bind.OrganizationUUID, WorkspaceUUID: bind.WorkspaceUUID,
		SessionUUID: bind.SessionUUID, SessionExternalID: bind.SessionExternalID, IdentityUUID: bind.IdentityUUID,
		AgentExternalID: bind.AgentExternalID, BindingRevision: bind.BindingRevision, ConfigRevision: bind.ConfigRevision,
		PersonalVaultID: bind.PersonalVaultID, ScopeKind: bind.ScopeKind, ScopeKey: bind.ScopeKey,
		ChannelExternalID: bind.ChannelExternalID,
	})
}

func (s *Service) ExecutionView(ctx context.Context, workspaceUUID, channelExternalID string) (map[string]any, error) {
	row, err := s.store.FindExecutionContext(ctx, workspaceUUID, channelExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view := map[string]any{"mode": row.Mode, "connection_status": row.ConnectionStatus, "entry_closed": row.EntryClosed, "revision": row.Revision}
	if row.AgentVersion != nil {
		view["agent_version"] = *row.AgentVersion
	}
	if row.IdentityUUID != nil {
		if identity, identityErr := s.store.FindBusinessIdentityByUUID(ctx, workspaceUUID, *row.IdentityUUID); identityErr == nil {
			view["identity_id"] = identity.ExternalID
			view["identity_enabled"] = identity.Enabled
		}
	}
	return view, nil
}

func (s *Service) ListAgentBindings(ctx context.Context, principal auth.Principal, agentID string) ([]db.AgentBindingRecord, error) {
	if _, err := s.visibleAgent(ctx, principal.WorkspaceUUID, agentID, 0); err != nil {
		return nil, err
	}
	rows, err := s.store.ListAgentIdentityBindings(ctx, principal.WorkspaceUUID, agentID)
	return rows, mapStore(err, "Agent 不存在")
}

func (s *Service) ListRecoverableSessions(ctx context.Context, principal auth.Principal, identityID, scopeKey, personalVaultID string) ([]db.SessionIdentityRecord, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListIdentitySessions(ctx, principal.WorkspaceUUID, identity.UUID, pageMax)
	if err != nil {
		return nil, unavailable(err)
	}
	if !identity.Enabled {
		return []db.SessionIdentityRecord{}, nil
	}
	kept := make([]db.SessionIdentityRecord, 0, len(rows))
	for _, row := range rows {
		binding := SessionBinding{
			IdentityID: identity.ExternalID, AgentID: row.AgentExternalID, BindingRevision: row.BindingRevision,
			ConfigRevision: row.ConfigRevision, PersonalVaultID: row.PersonalVaultID, ScopeKey: row.ScopeKey,
		}
		entry := EntryState{
			IdentityID: identity.ExternalID, AgentID: row.AgentExternalID, BindingRevision: row.BindingRevision,
			ConfigRevision: row.ConfigRevision, PersonalVaultID: personalVaultID, ScopeKey: scopeKey, IdentityEnabled: true,
		}
		if personalVaultID == "" {
			entry.PersonalVaultID = row.PersonalVaultID
		}
		if scopeKey == "" {
			entry.ScopeKey = row.ScopeKey
		}
		if SessionCurrent(binding, entry) {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

func sameExecution(current db.ExecutionContextRecord, identityUUID *string, version *int) bool {
	return sameStringPtr(current.IdentityUUID, identityUUID) && sameVersion(current.AgentVersion, version)
}

func sameStringPtr(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
