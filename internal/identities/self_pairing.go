package identities

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) AllowsSelfPairing(ctx context.Context, ingress Ingress) (bool, error) {
	execution, err := s.store.FindExecutionContext(ctx, ingress.WorkspaceUUID, ingress.ChannelExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	return execution.Mode == "pairing" && !execution.EntryClosed, err
}

func (s *Service) PrepareSelfPairing(ctx context.Context, in Ingress) (string, error) {
	allows, err := s.AllowsSelfPairing(ctx, in)
	if err != nil {
		return "", err
	}
	if !allows {
		return "", db.ErrInvalidState
	}
	in.ScopeKind = "direct"
	in.RemoteID = in.ActorID
	p, _, _, err := s.ensurePendingPairing(ctx, in)
	if err != nil {
		return "", err
	}
	if p.Status == "active" {
		return "", nil
	}
	if p.Status != "pending" {
		return "", db.ErrInvalidState
	}
	return p.ExternalID, nil
}

func (s *Service) ConfirmSelfPairing(ctx context.Context, ingress Ingress, pairingID string, version *int) error {
	execution, err := s.store.FindExecutionContext(ctx, ingress.WorkspaceUUID, ingress.ChannelExternalID)
	legacy := errors.Is(err, db.ErrNotFound) && ingress.ChannelMode == "pairing"
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return unavailable(err)
	}
	if !legacy && (execution.Mode != "pairing" || execution.EntryClosed) {
		return conflict("当前渠道未开放私聊自助配对", nil)
	}
	row, err := s.store.FindScopePairing(ctx, ingress.WorkspaceUUID, pairingID)
	if err != nil {
		return mapStore(err, "配对不存在，请重新发送消息")
	}
	if row.ChannelUUID != ingress.ChannelUUID || row.PlatformAccount != ingress.AppID || row.ScopeKind != "direct" || row.RemoteID != ingress.ActorID || row.Status == "unbound" {
		return conflict("配对已失效，请重新发送消息", nil)
	}
	if row.Status == "active" {
		owner, err := s.PersonalActorIdentity(ctx, ingress, false)
		if err != nil {
			return unavailable(err)
		}
		if row.IdentityUUID == nil || owner.UUID != *row.IdentityUUID {
			return conflict("当前私聊已使用其他绑定，请联系管理员", nil)
		}
		return nil
	}
	return s.activatePersonalPairing(ctx, ingress, row, version)
}

func (s *Service) activatePersonalPairing(ctx context.Context, ingress Ingress, row db.ScopePairingRecord, version *int) error {
	principal := auth.Principal{OrganizationUUID: ingress.OrganizationUUID, WorkspaceUUID: ingress.WorkspaceUUID}
	if err := s.validatePairingAgent(ctx, principal, ingress.AgentExternalID, version); err != nil {
		return err
	}
	identity, err := s.PersonalActorIdentity(ctx, ingress, true)
	if err != nil {
		return err
	}
	if !identity.Enabled {
		return conflict("业务身份已停用，请联系管理员", nil)
	}
	err = s.activatePairing(ctx, principal, row, identity, PairingWrite{AgentID: ingress.AgentExternalID, AgentVersion: version})
	if errors.Is(err, db.ErrInvalidState) {
		current, findErr := s.store.FindScopePairing(ctx, ingress.WorkspaceUUID, row.ExternalID)
		if findErr != nil {
			return findErr
		}
		if current.Status == "active" && sameScopeTarget(current, identity.UUID, ingress.AgentExternalID, version) {
			return nil
		}
		if current.Status == "active" {
			return conflict("配对目标已变化，请重新发送消息", nil)
		}
	}
	return err
}

func sameScopeTarget(row db.ScopePairingRecord, identityUUID, agentID string, version *int) bool {
	return row.IdentityUUID != nil && *row.IdentityUUID == identityUUID && row.AgentExternalID == agentID && sameVersion(row.AgentVersion, version)
}

func (s *Service) PersonalActorIdentity(ctx context.Context, ingress Ingress, create bool) (db.IdentityRecord, error) {
	identity, err := s.store.FindPersonalIdentityByActor(ctx, ingress.OrganizationUUID, ingress.WorkspaceUUID, ingress.ChannelType, ingress.AppID, ingress.ActorID)
	if !errors.Is(err, db.ErrNotFound) || !create {
		return identity, err
	}
	principal := auth.Principal{OrganizationUUID: ingress.OrganizationUUID, WorkspaceUUID: ingress.WorkspaceUUID}
	namespace := ingress.ChannelType + ":" + ingress.AppID
	identity, err = s.createIdentity(ctx, principal, IdentityInput{Namespace: namespace, SubjectID: ingress.ActorID, DisplayName: ingress.DisplayName})
	if errors.Is(err, db.ErrDuplicate) {
		rows, findErr := s.store.ListBusinessIdentities(ctx, db.ListIdentityParams{
			WorkspaceUUID: ingress.WorkspaceUUID, Namespace: namespace, Subject: ingress.ActorID, Limit: 1,
		})
		if findErr != nil {
			return db.IdentityRecord{}, unavailable(findErr)
		}
		if len(rows) == 0 {
			return db.IdentityRecord{}, conflict("执行身份发生变化，请重试", nil)
		}
		identity, err = rows[0], nil
	}
	if err != nil {
		return db.IdentityRecord{}, err
	}
	if err := s.store.ClaimPersonalIdentityByActor(ctx, ingress.OrganizationUUID, ingress.WorkspaceUUID, ingress.ChannelType, ingress.AppID, ingress.ActorID, identity.UUID); err != nil {
		return db.IdentityRecord{}, unavailable(err)
	}
	return s.store.FindPersonalIdentityByActor(ctx, ingress.OrganizationUUID, ingress.WorkspaceUUID, ingress.ChannelType, ingress.AppID, ingress.ActorID)
}
