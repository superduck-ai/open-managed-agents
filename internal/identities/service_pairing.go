package identities

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type PairingWrite struct {
	Code         string
	PairingID    string
	IdentityID   string
	AgentID      string
	AgentVersion *int
	Revision     int64
}

func (s *Service) ListPairings(ctx context.Context, principal auth.Principal, channelID, status, cursor string, limit int) ([]map[string]any, string, error) {
	limit = pageLimit(limit)
	rows, err := s.store.ListScopePairings(ctx, db.ListScopePairingParams{
		WorkspaceUUID: principal.WorkspaceUUID, ChannelExternalID: channelID, Status: status, Cursor: cursor, Limit: limit + 1,
	})
	if err != nil {
		return nil, "", unavailable(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].ExternalID
		rows = rows[:limit]
	}
	views := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		views = append(views, s.pairingView(ctx, principal.WorkspaceUUID, row))
	}
	return views, next, nil
}

func (s *Service) GetPairing(ctx context.Context, principal auth.Principal, pairingID string) (map[string]any, error) {
	row, err := s.store.FindScopePairing(ctx, principal.WorkspaceUUID, pairingID)
	if err != nil {
		return nil, mapStore(err, "配对不存在")
	}
	return s.pairingView(ctx, principal.WorkspaceUUID, row), nil
}

func (s *Service) CompletePairing(ctx context.Context, principal auth.Principal, input PairingWrite) (map[string]any, error) {
	if (input.Code == "") == (input.PairingID == "") {
		return nil, invalid("配对码和配对编号必须且只能提供一个", nil)
	}
	if input.IdentityID == "" || input.AgentID == "" {
		return nil, invalid("请同时指定执行身份和智能体", nil)
	}
	identity, err := s.GetIdentity(ctx, principal, input.IdentityID)
	if err != nil {
		return nil, err
	}
	if !identity.Enabled {
		return nil, conflict("业务身份已停用，不能完成配对", nil)
	}
	if _, err := s.visibleAgent(ctx, principal.WorkspaceUUID, input.AgentID, versionValue(input.AgentVersion)); err != nil {
		return nil, err
	}
	if input.Code != "" {
		return s.completeByCode(ctx, principal, identity, input)
	}
	return s.completeByID(ctx, principal, identity, input)
}

func (s *Service) completeByCode(ctx context.Context, principal auth.Principal, identity db.IdentityRecord, input PairingWrite) (map[string]any, error) {
	now := time.Now().UTC()
	row, err := s.store.FindPendingScopePairingByCode(ctx, principal.WorkspaceUUID, HashPairingCode(input.Code), now)
	if errors.Is(err, db.ErrNotFound) {
		return nil, conflict("配对码已过期或不能再次激活", err)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if err := s.store.ActivateScopePairingByCode(ctx, db.ActivateScopePairingParams{
		WorkspaceUUID: principal.WorkspaceUUID, ExternalID: row.ExternalID, IdentityUUID: identity.UUID,
		AgentExternalID: input.AgentID, AgentVersion: input.AgentVersion, CodeHash: HashPairingCode(input.Code), Now: now,
	}); err != nil {
		return nil, mapStore(err, "配对不存在")
	}
	s.linkDirectActor(ctx, principal, row, identity.UUID)
	return s.GetPairing(ctx, principal, row.ExternalID)
}

func (s *Service) completeByID(ctx context.Context, principal auth.Principal, identity db.IdentityRecord, input PairingWrite) (map[string]any, error) {
	row, err := s.store.FindScopePairing(ctx, principal.WorkspaceUUID, input.PairingID)
	if err != nil {
		return nil, mapStore(err, "配对不存在")
	}
	_, changed, activateErr := ActivateWithID(pairingFromRecord(row), identity.UUID, input.AgentID, input.AgentVersion)
	if activateErr != nil {
		return nil, conflict("过期或已解除的配对不能被重新激活", activateErr)
	}
	if changed {
		if err := s.store.ActivateScopePairingByID(ctx, db.ActivateScopePairingParams{
			WorkspaceUUID: principal.WorkspaceUUID, ExternalID: row.ExternalID, IdentityUUID: identity.UUID,
			AgentExternalID: input.AgentID, AgentVersion: input.AgentVersion,
		}); err != nil {
			return nil, mapStore(err, "配对不存在")
		}
		s.linkDirectActor(ctx, principal, row, identity.UUID)
	}
	return s.GetPairing(ctx, principal, row.ExternalID)
}

func (s *Service) UpdatePairing(ctx context.Context, principal auth.Principal, pairingID string, input PairingWrite) (map[string]any, error) {
	if input.IdentityID == "" && input.AgentID == "" && input.AgentVersion == nil {
		return nil, invalid("请至少修改执行身份、智能体或版本中的一项", nil)
	}
	row, err := s.store.FindScopePairing(ctx, principal.WorkspaceUUID, pairingID)
	if err != nil {
		return nil, mapStore(err, "配对不存在")
	}
	identityID := input.IdentityID
	identityUUID := ""
	if row.IdentityUUID != nil {
		identityUUID = *row.IdentityUUID
	}
	if identityID != "" {
		identity, identityErr := s.GetIdentity(ctx, principal, identityID)
		if identityErr != nil {
			return nil, identityErr
		}
		if !identity.Enabled {
			return nil, conflict("业务身份已停用，不能改绑", nil)
		}
		identityUUID = identity.UUID
	}
	agentID := input.AgentID
	if agentID == "" {
		agentID = row.AgentExternalID
	}
	version := row.AgentVersion
	if input.AgentVersion != nil {
		version = input.AgentVersion
	}
	if _, err := s.visibleAgent(ctx, principal.WorkspaceUUID, agentID, versionValue(version)); err != nil {
		return nil, err
	}
	_, changed, rebindErr := Rebind(pairingFromRecord(row), input.Revision, identityUUID, agentID, version)
	if rebindErr != nil {
		return nil, conflict("修订已变化或配对已不能更新", rebindErr)
	}
	if changed {
		if err := s.store.RebindScopePairing(ctx, db.RebindScopePairingParams{
			WorkspaceUUID: principal.WorkspaceUUID, ExternalID: pairingID, IdentityUUID: identityUUID,
			AgentExternalID: agentID, AgentVersion: version, ExpectedRevision: input.Revision,
		}); err != nil {
			return nil, mapStore(err, "配对不存在")
		}
	}
	return s.GetPairing(ctx, principal, pairingID)
}

func (s *Service) UnbindPairing(ctx context.Context, principal auth.Principal, pairingID string, revision int64) (map[string]any, error) {
	row, err := s.store.FindScopePairing(ctx, principal.WorkspaceUUID, pairingID)
	if err != nil {
		return nil, mapStore(err, "配对不存在")
	}
	_, changed, unbindErr := Unbind(pairingFromRecord(row), revision)
	if unbindErr != nil {
		return nil, conflict("修订已变化或配对已不能解除", unbindErr)
	}
	if changed {
		if err := s.store.UnbindScopePairing(ctx, principal.WorkspaceUUID, pairingID, revision); err != nil {
			return nil, mapStore(err, "配对不存在")
		}
	}
	return s.GetPairing(ctx, principal, pairingID)
}

func (s *Service) linkDirectActor(ctx context.Context, principal auth.Principal, row db.ScopePairingRecord, identityUUID string) {
	if row.ScopeKind != "direct" {
		return
	}
	if err := s.store.UpsertIdentityActorLink(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, "feishu", row.PlatformAccount, row.RemoteID, identityUUID); err != nil {
		s.logger.WarnContext(ctx, "link channel actor", "pairing_id", row.ExternalID, "error", err)
	}
}

func (s *Service) pairingView(ctx context.Context, workspaceUUID string, row db.ScopePairingRecord) map[string]any {
	view := map[string]any{
		"id": row.ExternalID, "channel_id": row.ChannelExternalID, "scope_kind": row.ScopeKind,
		"remote_id": row.RemoteID, "status": row.Status, "agent_id": row.AgentExternalID,
		"revision": row.Revision, "display_name": row.DisplayName,
	}
	if row.AgentVersion != nil {
		view["agent_version"] = *row.AgentVersion
	}
	if row.IdentityUUID != nil {
		if identity, err := s.store.FindBusinessIdentityByUUID(ctx, workspaceUUID, *row.IdentityUUID); err == nil {
			view["identity_id"] = identity.ExternalID
		}
	}
	if row.CodeExpiresAt != nil && row.Status == "pending" {
		view["code_expires_at"] = row.CodeExpiresAt.UTC().Format(time.RFC3339)
	}
	return view
}

func pairingFromRecord(row db.ScopePairingRecord) Pairing {
	identityID := ""
	if row.IdentityUUID != nil {
		identityID = *row.IdentityUUID
	}
	pairing := Pairing{Status: row.Status, IdentityID: identityID, AgentID: row.AgentExternalID, AgentVersion: row.AgentVersion, Revision: row.Revision}
	if row.CodeExpiresAt != nil {
		pairing.CodeExpires = *row.CodeExpiresAt
	}
	return pairing
}

func versionValue(version *int) int {
	if version == nil {
		return 0
	}
	return *version
}

func (s *Service) openPairing(ctx context.Context, ingress Ingress, create bool) (db.ScopePairingRecord, string, bool, error) {
	existing, err := s.store.FindOpenScopePairing(ctx, ingress.ChannelUUID, ingress.ScopeKind, ingress.AppID, ingress.RemoteID)
	if err == nil {
		return existing, "", false, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.ScopePairingRecord{}, "", false, err
	}
	if !create {
		return db.ScopePairingRecord{Status: "pending"}, "", false, nil
	}
	return s.ensurePendingPairing(ctx, ingress)
}

func (s *Service) ensurePendingPairing(ctx context.Context, ingress Ingress) (db.ScopePairingRecord, string, bool, error) {
	existing, err := s.store.FindOpenScopePairing(ctx, ingress.ChannelUUID, ingress.ScopeKind, ingress.AppID, ingress.RemoteID)
	if err == nil {
		return existing, "", false, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.ScopePairingRecord{}, "", false, err
	}
	code, err := NewPairingCode()
	if err != nil {
		return db.ScopePairingRecord{}, "", false, err
	}
	externalID, err := ids.New("chsp_")
	if err != nil {
		return db.ScopePairingRecord{}, "", false, err
	}
	created, err := s.store.CreateScopePairing(ctx, db.InsertScopePairingParams{
		UUID: uuid.NewString(), ExternalID: externalID, OrganizationUUID: ingress.OrganizationUUID,
		WorkspaceUUID: ingress.WorkspaceUUID, ChannelUUID: ingress.ChannelUUID, ChannelExternalID: ingress.ChannelExternalID,
		ScopeKind: ingress.ScopeKind, PlatformAccount: ingress.AppID, RemoteID: ingress.RemoteID, DisplayName: ingress.DisplayName,
		CodeHash: HashPairingCode(code), CodeExpiresAt: time.Now().UTC().Add(pairingCodeTTL),
	})
	if errors.Is(err, db.ErrDuplicate) {
		existing, findErr := s.store.FindOpenScopePairing(ctx, ingress.ChannelUUID, ingress.ScopeKind, ingress.AppID, ingress.RemoteID)
		return existing, "", false, findErr
	}
	return created, code, true, err
}
