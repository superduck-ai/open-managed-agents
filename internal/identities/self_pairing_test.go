package identities

import (
	"context"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSelfPairingUsesActorIdentityAndKeepsOriginalTarget(t *testing.T) {
	store := &selfPairingStore{
		acceptanceStore: newAcceptanceStore(),
		pairings:        map[string]db.ScopePairingRecord{},
		version:         new(4),
		links:           map[string]string{},
	}
	service := NewService(store, nil)
	ingress := Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_self",
		ChannelType: "feishu", AppID: "app", ChatType: "p2p", ActorID: "ou_left", DisplayName: "张三",
		AgentExternalID: "agent_catalog",
	}
	preview, err := service.Preview(t.Context(), ingress)
	if err != nil || preview.Mode != "hold" || len(store.pairings) != 0 || len(store.links) != 0 {
		t.Fatalf("预览创建了配对或身份：%+v %v", preview, err)
	}
	pending, err := service.PrepareSelfPairing(t.Context(), ingress)
	if err != nil || pending == "" || store.pairings[pending].Status != "pending" || len(store.links) != 0 {
		t.Fatalf("授权前应仅保存待配对记录：%q %v", pending, err)
	}
	if err := service.ConfirmSelfPairing(t.Context(), ingress, pending, store.version); err != nil {
		t.Fatal(err)
	}
	if again, err := service.PrepareSelfPairing(t.Context(), ingress); err != nil || again != "" {
		t.Fatalf("已有配对仍要求重新授权：%q %v", again, err)
	}
	owner, err := service.PersonalActorIdentity(t.Context(), ingress, true)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := service.Resolve(t.Context(), ingress)
	if err != nil || decision.Mode != "execute" || decision.IdentityUUID != owner.UUID || decision.AgentVersion != 4 {
		t.Fatalf("首条私聊没有使用已有个人身份和固定版本执行：%+v %v", decision, err)
	}
	left, err := store.FindOpenScopePairing(t.Context(), ingress.ChannelUUID, "direct", ingress.AppID, ingress.ActorID)
	if err != nil {
		t.Fatal(err)
	}
	first := left.ExternalID
	ingress.AgentExternalID = "agent_new"
	store.version = nil
	again, err := service.Resolve(t.Context(), ingress)
	if err != nil || again.AgentExternalID != "agent_catalog" || again.AgentVersion != 4 {
		t.Fatalf("已有配对目标被覆盖：%+v %v", again, err)
	}
	other := ingress
	other.ActorID, other.DisplayName = "ou_right", "李四"
	if err := service.ConfirmSelfPairing(t.Context(), other, first, nil); err == nil {
		t.Fatal("其他用户能操作旧确认卡片")
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
	current, err := store.FindScopePairing(t.Context(), "ws", first)
	if err != nil || current.AgentExternalID != "agent_catalog" || current.AgentVersion == nil || *current.AgentVersion != 4 || current.Revision != 1 {
		t.Fatalf("重复确认覆盖了已有配对: %+v %v", current, err)
	}
	second, err := service.Resolve(t.Context(), other)
	if err != nil || second.Mode != "execute" || second.IdentityUUID == owner.UUID {
		t.Fatalf("不同操作者没有创建独立身份：%+v %v", second, err)
	}
	left.Status = "unbound"
	store.pairings[first] = left
	if err := service.ConfirmSelfPairing(t.Context(), ingress, first, nil); err == nil {
		t.Fatal("旧卡片恢复了已解绑配对")
	}
	disabled := store.identities[owner.UUID]
	disabled.Enabled = false
	store.identities[owner.UUID] = disabled
	held, err := service.Resolve(t.Context(), ingress)
	if err != nil || held.Mode != "hold" {
		t.Fatalf("自动配对绕过身份停用：%+v %v", held, err)
	}
	disabled.Enabled = true
	store.identities[owner.UUID] = disabled
	rebound, err := service.Resolve(t.Context(), ingress)
	if err != nil || rebound.Mode != "execute" || rebound.IdentityUUID != owner.UUID || rebound.AgentExternalID != "agent_new" {
		t.Fatalf("重新配对应复用身份并使用当前执行目标：%+v %v", rebound, err)
	}
}

func TestLegacyChannelKeepsAdmissionAndOnlyResumesMatchingConfirmedInput(t *testing.T) {
	store := newAcceptanceStore()
	service := NewService(store, nil)
	ingress := Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_legacy",
		ChannelType: "feishu", ChannelMode: "pairing", ChatType: "p2p", AppID: "app", ActorID: "ou_alice",
	}
	allow, err := service.AllowsSelfPairing(t.Context(), ingress)
	if err != nil || allow {
		t.Fatalf("旧准入模式启用了新自助配对：%v %v", allow, err)
	}
	decision, err := service.Resolve(t.Context(), ingress)
	if err != nil || decision.Mode != "legacy" {
		t.Fatalf("旧渠道的新消息使用了范围配对：%+v %v", decision, err)
	}
	decision, err = service.Preview(t.Context(), ingress)
	if err != nil || decision.Mode != "legacy" || store.pairingCreates != 0 {
		t.Fatalf("旧历史恢复被身份配对拦截：%+v %v", decision, err)
	}
	ingress.ConfirmedPairingID = "chsp_other"
	decision, err = service.Resolve(t.Context(), ingress)
	if err != nil || decision.Mode != "hold" {
		t.Fatalf("旧消息可以换用其他配对：%+v %v", decision, err)
	}
	ingress.ConfirmedPairingID = "chsp_alice"
	decision, err = service.Resolve(t.Context(), ingress)
	if err != nil || decision.Mode != "execute" || decision.IdentityUUID != "id_catalog" {
		t.Fatalf("匹配的旧确认没有续跑原输入：%+v %v", decision, err)
	}
}

func TestSelfPairingRejectsConcurrentDifferentTarget(t *testing.T) {
	store := &selfPairingStore{acceptanceStore: newAcceptanceStore(), pairings: map[string]db.ScopePairingRecord{}, links: map[string]string{}}
	store.activateErr = db.ErrInvalidState
	service := NewService(store, nil)
	decision, err := service.Resolve(t.Context(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_self",
		ChannelType: "feishu", AppID: "app", ChatType: "p2p", ActorID: "ou_left", DisplayName: "张三", AgentExternalID: "agent_catalog",
	})
	if err != nil || decision.Mode != "hold" || !strings.Contains(decision.Notice, "配对目标已变化") {
		t.Fatalf("并发改到其他目标后仍继续原消息：%+v %v", decision, err)
	}
}

type selfPairingStore struct {
	*acceptanceStore
	pairings    map[string]db.ScopePairingRecord
	links       map[string]string
	version     *int
	activateErr error
}

func (s *selfPairingStore) FindExecutionContext(context.Context, string, string) (db.ExecutionContextRecord, error) {
	return db.ExecutionContextRecord{Mode: "pairing", AgentVersion: s.version}, nil
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
	if s.activateErr != nil {
		err := s.activateErr
		s.activateErr = nil
		row := s.pairings[p.ExternalID]
		other := "id_other"
		row.Status = "active"
		row.IdentityUUID = &other
		row.AgentExternalID = "agent_other"
		s.pairings[p.ExternalID] = row
		return err
	}
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
