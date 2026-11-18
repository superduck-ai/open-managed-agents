package identities

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSelfPairingUsesActorIdentityAndKeepsOriginalTarget(t *testing.T) {
	store := &selfPairingStore{
		acceptanceStore: newAcceptanceStore(),
		pairings:        map[string]db.ScopePairingRecord{},
		links:           map[string]string{},
	}
	service := NewService(store, nil)
	ingress := Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_self",
		ChannelType: "feishu", AppID: "app", ChatType: "p2p", ActorID: "ou_left", DisplayName: "张三",
		AgentExternalID: "agent_friday",
	}
	first, err := service.PrepareSelfPairing(t.Context(), ingress)
	if err != nil || first == "" {
		t.Fatalf("创建待配对失败: %s %v", first, err)
	}
	other := ingress
	other.ActorID, other.DisplayName = "ou_right", "李四"
	if err := service.ConfirmSelfPairing(t.Context(), other, first, nil); err == nil {
		t.Fatal("其他用户能操作张三的配对")
	}
	owner, err := service.PersonalActorIdentity(t.Context(), ingress, true)
	if err != nil {
		t.Fatal(err)
	}
	version := 4
	if err := service.ConfirmSelfPairing(t.Context(), ingress, first, &version); err != nil {
		t.Fatal(err)
	}
	ingress.AgentExternalID = "agent_new"
	if err := service.ConfirmSelfPairing(t.Context(), ingress, first, nil); err != nil {
		t.Fatal(err)
	}
	left := store.pairings[first]
	if left.IdentityUUID == nil || *left.IdentityUUID != owner.UUID {
		t.Fatal("OAuth 先分配的个人身份未被配对复用")
	}
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	if _, err := service.UpdatePairing(t.Context(), principal, first, PairingWrite{IdentityID: "idn_right", Revision: 1}); err == nil {
		t.Fatal("管理操作把个人配对改到了另一个身份")
	}
	if _, err := service.UpdateIdentity(t.Context(), principal, owner.ExternalID, IdentityInput{SubjectID: "ou_other"}); err == nil {
		t.Fatal("普通身份编辑可以更换个人身份的归属")
	}
	if err := service.DeleteIdentity(t.Context(), principal, owner.ExternalID); err == nil {
		t.Fatal("普通删除可以清除个人身份的稳定归属")
	}
	if left.AgentExternalID != "agent_friday" || left.AgentVersion == nil || *left.AgentVersion != 4 || left.Revision != 1 {
		t.Fatalf("重复确认覆盖了已有配对: %+v", left)
	}
	second, err := service.PrepareSelfPairing(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmSelfPairing(t.Context(), other, second, nil); err != nil {
		t.Fatal(err)
	}
	if *left.IdentityUUID == *store.pairings[second].IdentityUUID {
		t.Fatal("两个操作者共享同一个自动分配的身份")
	}
	left.Status = "unbound"
	store.pairings[first] = left
	if err := service.ConfirmSelfPairing(t.Context(), ingress, first, nil); err == nil {
		t.Fatal("旧卡片恢复了已解绑配对")
	}
	rebound, err := service.PrepareSelfPairing(t.Context(), ingress)
	if err != nil || rebound == first {
		t.Fatalf("解绑后未取得新配对: %s %v", rebound, err)
	}
	disabled := store.identities[*left.IdentityUUID]
	disabled.Enabled = false
	store.identities[disabled.UUID] = disabled
	if err := service.ConfirmSelfPairing(t.Context(), ingress, rebound, nil); err == nil {
		t.Fatal("自助配对绕过身份停用")
	}
	disabled.Enabled = true
	store.identities[disabled.UUID] = disabled
	if err := service.ConfirmSelfPairing(t.Context(), ingress, rebound, nil); err != nil {
		t.Fatal(err)
	}
	if *store.pairings[rebound].IdentityUUID != *left.IdentityUUID || store.pairings[rebound].AgentExternalID != "agent_new" {
		t.Fatal("重新配对应复用身份并使用当前执行目标")
	}
}

type selfPairingStore struct {
	*acceptanceStore
	pairings map[string]db.ScopePairingRecord
	links    map[string]string
}

func (s *selfPairingStore) FindExecutionContext(context.Context, string, string) (db.ExecutionContextRecord, error) {
	return db.ExecutionContextRecord{Mode: "pairing"}, nil
}
func (s *selfPairingStore) FindOpenScopePairing(_ context.Context, channel, kind, app, actor string) (db.ScopePairingRecord, error) {
	for _, row := range s.pairings {
		if row.ChannelUUID == channel && row.ScopeKind == kind && row.PlatformAccount == app && row.RemoteID == actor && row.Status != "unbound" {
			return row, nil
		}
	}
	return db.ScopePairingRecord{}, db.ErrNotFound
}
func (s *selfPairingStore) CreateScopePairing(_ context.Context, p db.InsertScopePairingParams) (db.ScopePairingRecord, error) {
	row := db.ScopePairingRecord{ExternalID: p.ExternalID, ScopeKind: p.ScopeKind, ChannelUUID: p.ChannelUUID,
		PlatformAccount: p.PlatformAccount, RemoteID: p.RemoteID, DisplayName: p.DisplayName, Status: "pending"}
	s.pairings[p.ExternalID] = row
	return row, nil
}
func (s *selfPairingStore) FindScopePairing(_ context.Context, _, id string) (db.ScopePairingRecord, error) {
	if row, ok := s.pairings[id]; ok {
		return row, nil
	}
	return db.ScopePairingRecord{}, db.ErrNotFound
}
func (s *selfPairingStore) CreateBusinessIdentity(_ context.Context, p db.InsertIdentityParams) (db.IdentityRecord, error) {
	row := db.IdentityRecord{ExternalID: p.ExternalID, UUID: p.UUID, Enabled: p.Enabled}
	s.identities[p.UUID] = row
	return row, nil
}
func (s *selfPairingStore) ActivateScopePairingByID(_ context.Context, p db.ActivateScopePairingParams) error {
	row := s.pairings[p.ExternalID]
	row.IdentityUUID, row.AgentExternalID, row.AgentVersion = &p.IdentityUUID, p.AgentExternalID, p.AgentVersion
	row.Revision, row.Status = 1, "active"
	s.pairings[p.ExternalID] = row
	return nil
}
func (s *selfPairingStore) FindPersonalIdentityByActor(ctx context.Context, _, workspace, _, app, actor string) (db.IdentityRecord, error) {
	return s.FindBusinessIdentityByUUID(ctx, workspace, s.links[app+":"+actor])
}
func (s *selfPairingStore) ClaimPersonalIdentityByActor(_ context.Context, _, _, _, app, actor, identity string) error {
	s.links[app+":"+actor] = identity
	return nil
}

func (s *selfPairingStore) IsPersonalIdentity(_ context.Context, _, identity string) (bool, error) {
	for _, owner := range s.links {
		if owner == identity {
			return true, nil
		}
	}
	return false, nil
}
