package channels

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/identities"
)

type identityIngress interface {
	PersonalActorIdentity(context.Context, identities.Ingress, bool) (db.IdentityRecord, error)
	AllowsSelfPairing(context.Context, identities.Ingress) (bool, error)
	ConfirmSelfPairing(context.Context, identities.Ingress, string, *int) error
	Resolve(context.Context, identities.Ingress) (identities.Decision, error)
	Preview(context.Context, identities.Ingress) (identities.Decision, error)
	ObserveConnection(context.Context, string, string, string) error
	ExecutionView(context.Context, string, string) (map[string]any, error)
	ResolveChannelIdentity(context.Context, auth.Principal, string) (string, error)
}

func (s *Service) UseIdentities(ingress identityIngress) {
	if s == nil {
		return
	}
	if s.supervisor != nil {
		s.supervisor.ingress = ingress
	}
	if s.local != nil {
		s.local.ingress = ingress
	}
}

func (w *channelWorker) observeOnline(online bool) {
	w.online.Store(online)
	if w.supervisor.ingress == nil {
		return
	}
	status := "disconnected"
	if online {
		status = "healthy"
	}
	_ = w.supervisor.ingress.ObserveConnection(context.Background(), w.record.WorkspaceUUID, w.record.ExternalID, status)
}

func (w *channelWorker) executionDecision(ctx context.Context, msg *channelMessage, scopeKey string, create bool) (identities.Decision, error) {
	if create && w.supervisor.ingress == nil {
		return identities.Decision{Mode: "legacy"}, nil
	}
	personalID := ""
	personalActive := false
	if msg.ChatType == "p2p" {
		var err error
		if create && w.supervisor.personalVault != nil {
			personalID, err = w.supervisor.personalVault(ctx, w.record, msg.UserID)
			personalActive = err == nil && personalID != ""
		} else if w.supervisor.personalVaultLookup != nil {
			personalID, personalActive, err = w.supervisor.personalVaultLookup(ctx, w.record, msg.UserID)
		}
		if create && errors.Is(err, errChannelPersonalIdentityDisabled) {
			return identities.Decision{Mode: "hold", Notice: "个人执行身份已停用，请联系管理员。"}, nil
		}
		if err != nil {
			return identities.Decision{}, err
		}
	}
	ingress := channelIngress(w.record, msg, scopeKey)
	ingress.PersonalVaultID = personalID
	ingress.PersonalActive = personalActive
	mounts := parseChannelMounts(w.record.Mounts)
	ingress.SharedVaultIDs = mounts.VaultIDs
	if mounts.AgentVersion > 0 {
		ingress.AgentVersion = new(mounts.AgentVersion)
	}
	if create {
		return w.supervisor.ingress.Resolve(ctx, ingress)
	}
	if !personalActive {
		personalID = ""
	}
	if w.supervisor.ingress == nil {
		return identities.Decision{Mode: "legacy", PersonalVaultID: personalID}, nil
	}
	decision, err := w.supervisor.ingress.Preview(ctx, ingress)
	if decision.Mode == "legacy" {
		decision.PersonalVaultID = personalID
	}
	return decision, err
}

func channelIngress(record db.ChannelRecord, msg *channelMessage, scopeKey string) identities.Ingress {
	return identities.Ingress{
		OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID,
		ChannelUUID: record.UUID, ChannelExternalID: record.ExternalID, ChannelType: record.ChannelType, ChannelMode: record.Mode,
		AppID: record.AppID, AgentExternalID: record.AgentExternalID, ChatType: msg.ChatType,
		RemoteID: msg.ChatID, ActorID: msg.UserID, ScopeKey: scopeKey, DisplayName: msg.UserID,
	}
}

func (w *channelWorker) sessionForDecision(ctx context.Context, msg *channelMessage, scopeKey string, decision identities.Decision) (string, error) {
	environmentID, err := w.supervisor.store.GetChannelAgentEnvironment(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, decision.AgentExternalID)
	if err != nil {
		return "", err
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return "", err
	}
	sessionID, err := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return "", err
	}
	if sessionID != "" {
		session, sessionErr := w.supervisor.oma.getSession(ctx, apiKey, sessionID)
		if sessionErr != nil {
			return "", sessionErr
		}
		if session.ArchivedAt == nil && w.decisionCompatible(session, decision, environmentID, scopeKey) {
			bound, err := w.sessionIdentityBound(ctx, session.ID, decision, scopeKey)
			if err != nil {
				return "", err
			}
			if bound {
				return sessionID, nil
			}
		}
	}
	resources, err := channelSessionResources(ctx, w.supervisor.cipher, w.record)
	if err != nil {
		return "", err
	}
	resources.VaultIDs = append([]string{}, decision.VaultIDs...)
	metadata := channelSessionMetadata(w.record, msg, scopeKey)
	metadata["channel_resources"] = resources.Fingerprint
	metadata["credential_binding_mode"] = "first_vault"
	metadata["identity_id"] = decision.IdentityExternalID
	metadata["binding_revision"] = strconv.FormatInt(decision.BindingRevision, 10)
	metadata["config_revision"] = strconv.FormatInt(decision.ConfigRevision, 10)
	metadata["execution_agent_id"] = decision.AgentExternalID
	if decision.PersonalVaultID != "" {
		metadata["personal_vault_id"] = decision.PersonalVaultID
		metadata["oauth_vault_id"] = decision.PersonalVaultID
	}
	if decision.AgentVersion > 0 {
		metadata["agent_version"] = strconv.Itoa(decision.AgentVersion)
	}
	if w.supervisor.createIdentitySession == nil {
		return "", errors.New("渠道身份会话创建服务未配置")
	}
	body, err := json.Marshal(sessionCreationBody(decision.AgentExternalID, decision.AgentVersion, environmentID, channelSessionTitle(msg.Content), metadata, resources.Resources, resources.VaultIDs))
	if err != nil {
		return "", err
	}
	created, err := w.supervisor.createIdentitySession(ctx, apiKey, body, db.InsertSessionIdentityParams{
		IdentityUUID: decision.IdentityUUID, BindingRevision: decision.BindingRevision, ConfigRevision: decision.ConfigRevision,
		PersonalVaultID: decision.PersonalVaultID, ScopeKind: decision.ScopeKind, ScopeKey: scopeKey,
		ChannelExternalID: w.record.ExternalID,
	})
	if err != nil {
		return "", err
	}
	if err := w.supervisor.store.SetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey, created); err != nil {
		return "", err
	}
	return created, nil
}

func (w *channelWorker) sessionIdentityBound(ctx context.Context, sessionID string, decision identities.Decision, scopeKey string) (bool, error) {
	binding, err := w.supervisor.store.FindSessionIdentity(ctx, w.record.WorkspaceUUID, sessionID)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	return err == nil && binding.IdentityUUID == decision.IdentityUUID && binding.ChannelExternalID == w.record.ExternalID &&
		binding.ScopeKey == scopeKey && binding.BindingRevision == decision.BindingRevision, err
}

func (w *channelWorker) decisionCompatible(session omaSession, decision identities.Decision, environmentID, scopeKey string) bool {
	if session.Agent.ID != decision.AgentExternalID || session.EnvironmentID != environmentID {
		return false
	}
	if session.Metadata["identity_id"] != decision.IdentityExternalID || session.Metadata["scope_id"] != scopeKey {
		return false
	}
	if session.Metadata["binding_revision"] != strconv.FormatInt(decision.BindingRevision, 10) {
		return false
	}
	if session.Metadata["config_revision"] != strconv.FormatInt(decision.ConfigRevision, 10) {
		return false
	}
	if decision.PersonalVaultID != "" && session.Metadata["personal_vault_id"] != decision.PersonalVaultID {
		return false
	}
	if decision.AgentVersion > 0 && (session.Agent.Version != decision.AgentVersion || session.Metadata["agent_version"] != strconv.Itoa(decision.AgentVersion)) {
		return false
	}
	return session.Metadata["credential_binding_mode"] == "first_vault" && slices.Equal(session.VaultIDs, decision.VaultIDs) &&
		w.compatibleResources(session, parseChannelMounts(w.record.Mounts))
}

func (s *localService) attachExecution(ctx context.Context, view map[string]any, record db.ChannelRecord) error {
	if s.ingress == nil {
		return nil
	}
	extra, err := s.ingress.ExecutionView(ctx, record.WorkspaceUUID, record.ExternalID)
	if err != nil || extra == nil {
		return err
	}
	view["identity_resolution"] = map[string]any{"mode": extra["mode"]}
	view["connection_status"] = extra["connection_status"]
	view["entry_closed"] = extra["entry_closed"]
	if id, ok := extra["identity_id"]; ok {
		view["identity_id"] = id
	}
	return nil
}

func (s *localService) prepareExecution(ctx context.Context, principal auth.Principal, record *db.ChannelRecord, input channelMutation) error {
	mode, present, err := executionMode(input)
	if err != nil || !present {
		return err
	}
	identityID := ""
	if input.IdentityID != nil {
		identityID = *input.IdentityID
	}
	if (mode == "fixed") != (identityID != "") {
		return invalidChannelMode()
	}
	if record.ExecutionMode != nil && *record.ExecutionMode != mode {
		return immutableExecutionMode()
	}
	if mode == "fixed" {
		if s.ingress == nil {
			return unavailable(nil)
		}
		identityUUID, resolveErr := s.ingress.ResolveChannelIdentity(ctx, principal, identityID)
		if resolveErr != nil {
			return resolveErr
		}
		if record.EntryClosed && (record.IdentityUUID == nil || *record.IdentityUUID != identityUUID) {
			return closedExecutionEntry()
		}
		record.IdentityUUID = &identityUUID
	}
	record.ExecutionMode = &mode
	return nil
}

func executionMode(input channelMutation) (string, bool, error) {
	if len(input.IdentityResolution) == 0 && input.IdentityID == nil {
		return "", false, nil
	}
	mode := ""
	if len(input.IdentityResolution) > 0 && string(input.IdentityResolution) != "null" {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(input.IdentityResolution, &body); err != nil || (body.Mode != "fixed" && body.Mode != "pairing") {
			return "", true, invalidChannelMode()
		}
		mode = body.Mode
	}
	if input.IdentityID != nil && mode == "" {
		mode = "fixed"
	}
	if mode == "" {
		return "", true, invalidChannelMode()
	}
	return mode, true, nil
}

func filterChannelViews(rows []map[string]any, query url.Values) []map[string]any {
	channelType := query.Get("channel_type")
	enabled := query.Get("enabled")
	binding := query.Get("binding_status")
	identityID := query.Get("identity_id")
	if channelType == "" && enabled == "" && binding == "" && identityID == "" {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if channelType != "" && row["channel_type"] != channelType {
			continue
		}
		if binding != "" && row["binding_status"] != binding {
			continue
		}
		if identityID != "" && row["identity_id"] != identityID {
			continue
		}
		if enabled != "" {
			want := enabled == "true"
			value, _ := row["enabled"].(bool)
			if value != want {
				continue
			}
		}
		filtered = append(filtered, row)
	}
	return filtered
}
