package identities

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestAcceptanceDecisions(t *testing.T) {
	store := newAcceptanceStore()
	service := NewService(store, nil)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	left, err := service.Resolve(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_bot",
		ChatType: "group", RemoteID: "oc_left", ScopeKey: "chat:oc_left",
	})
	if err != nil || left.Mode != "execute" || left.AgentExternalID != "agent_left" || !contains(left.VaultIDs, "vlt_left") || contains(left.VaultIDs, "vlt_right") {
		t.Fatalf("left group: %v %#v", err, left)
	}
	right, err := service.Resolve(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_bot",
		ChatType: "group", RemoteID: "oc_right", ScopeKey: "chat:oc_right",
	})
	if err != nil || right.AgentExternalID != "agent_right" || contains(right.VaultIDs, "vlt_left") {
		t.Fatalf("right group leaked: %v %#v", err, right)
	}
	topicA, err := service.Preview(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_bot",
		ChatType: "group", RemoteID: "oc_left", ScopeKey: "thread:oc_left:a",
	})
	topicB, topicErr := service.Preview(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelUUID: "channel", ChannelExternalID: "chn_bot",
		ChatType: "group", RemoteID: "oc_left", ScopeKey: "thread:oc_left:b",
	})
	if err != nil || topicErr != nil || topicA.AgentExternalID != topicB.AgentExternalID || topicA.BindingRevision != topicB.BindingRevision || topicA.ScopeKey == topicB.ScopeKey {
		t.Fatalf("topics: %#v %#v %v %v", topicA, topicB, err, topicErr)
	}
	if store.pairingCreates != 0 {
		t.Fatal("preview created a pairing")
	}
	alice, err := service.Resolve(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelExternalID: "chn_direct", ChatType: "p2p", ActorID: "ou_alice",
		PersonalVaultID: "vlt_alice", PersonalActive: true, ScopeKey: "chat:ou_alice",
	})
	bob, bobErr := service.Resolve(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelExternalID: "chn_direct", ChatType: "p2p", ActorID: "ou_bob",
		PersonalVaultID: "vlt_bob", PersonalActive: true, ScopeKey: "chat:ou_bob",
	})
	if err != nil || bobErr != nil || alice.PersonalVaultID != "vlt_alice" || bob.PersonalVaultID != "vlt_bob" || contains(alice.VaultIDs, "vlt_bob") {
		t.Fatalf("personal vaults: %#v %#v %v %v", alice, bob, err, bobErr)
	}
	held, err := service.Resolve(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelExternalID: "chn_shared", ChatType: "p2p", ActorID: "ou_shared",
		PersonalVaultID: "vlt_alice", PersonalActive: true,
	})
	if err != nil || held.Mode != "hold" || contains(held.VaultIDs, "vlt_alice") {
		t.Fatalf("shared vault fell back: %v %#v", err, held)
	}
	friday := store.identities["id_friday"]
	friday.Enabled = false
	store.identities["id_friday"] = friday
	if err := service.AdmitExecution(context.Background(), "org", "ws", "ses_live", false); err != nil {
		t.Fatal(err)
	}
	if err := service.AdmitExecution(context.Background(), "org", "ws", "ses_live", true); err == nil {
		t.Fatal("disabled identity started a round")
	}
	if _, err := service.EnableIdentity(context.Background(), principal, "idn_friday"); err != nil {
		t.Fatal(err)
	}
	if store.channelMutations != 0 || !store.identities["id_friday"].Enabled {
		t.Fatalf("enable changed other resources: %d %#v", store.channelMutations, store.identities["id_friday"])
	}
	_, err = service.ClearIdentity(context.Background(), principal, "idn_friday")
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.Conflict || !strings.Contains(appErr.PublicMessage, "ses_running") {
		t.Fatalf("cleanup conflict: %v", err)
	}
	store.running = nil
	store.listConfigErr = errors.New("list failed")
	if _, err := service.ClearIdentity(context.Background(), principal, "idn_friday"); err == nil || store.channelMutations != 0 {
		t.Fatalf("cleanup continued after vault list failure: %v mutations=%d", err, store.channelMutations)
	}
	store.listConfigErr = nil
	store.vaultErr = errors.New("vault timeout")
	if _, err := service.Resolve(context.Background(), Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelExternalID: "chn_shared", ChatType: "p2p", ActorID: "ou_shared",
		PersonalVaultID: "vlt_alice", PersonalActive: true,
	}); err == nil {
		t.Fatal("vault query failure was treated as an archived mount")
	}
	store.vaultErr = nil
	store.archiveErr = db.ErrDuplicate
	cleared, err := service.ClearIdentity(context.Background(), principal, "idn_friday")
	if err != nil || cleared.Outcome.Completed || cleared.Outcome.PersonalVaultsDeleted || !contains(cleared.Outcome.RetainedSharedVaults, "vlt_shared") || contains(cleared.Outcome.RetainedSharedVaults, "vlt_left") {
		t.Fatalf("partial cleanup: %v %#v", err, cleared)
	}
	view := configView(db.IdentityConfigRecord{Env: `{"API_TOKEN":"sk-secret","REGION":"cn"}`, VaultMounts: `[]`, DisabledVaults: `[]`})
	env := view["env"].(map[string]string)
	if _, ok := env["API_TOKEN"]; ok || env["REGION"] != "cn" {
		t.Fatalf("config read leaked a secret: %#v", env)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type acceptanceStore struct {
	identities            map[string]db.IdentityRecord
	configs               map[string]db.IdentityConfigRecord
	pairings              map[string]db.ScopePairingRecord
	vaults                map[string]db.Vault
	running               []string
	pairingCreates        int
	channelMutations      int
	archiveErr            error
	listConfigErr         error
	vaultErr              error
	pendingCode           db.ScopePairingRecord
	pendingHash           string
	activated             *db.ScopePairingRecord
	rejectPairingList     bool
	pairingEnvironmentErr error
	activateErr           error
}

func newAcceptanceStore() *acceptanceStore {
	friday := "id_friday"
	left := "id_left"
	right := "id_right"
	now := time.Now().UTC()
	identity := func(id, external string, enabled bool) db.IdentityRecord {
		return db.IdentityRecord{UUID: id, ExternalID: external, OrganizationUUID: "org", WorkspaceUUID: "ws", Namespace: "crm", ExternalSubjectID: external, DisplayName: external, Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	}
	store := &acceptanceStore{
		identities: map[string]db.IdentityRecord{"id_friday": identity(friday, "idn_friday", true), left: identity(left, "idn_left", true), right: identity(right, "idn_right", true)},
		configs: map[string]db.IdentityConfigRecord{
			left + ":agent_left":     {ExternalID: "idcfg_left", IdentityUUID: left, AgentExternalID: "agent_left", Env: `{"REGION":"cn"}`, VaultMounts: `["vlt_left"]`, DisabledVaults: `[]`, Revision: 1, Status: "active"},
			right + ":agent_right":   {ExternalID: "idcfg_right", IdentityUUID: right, AgentExternalID: "agent_right", VaultMounts: `["vlt_right"]`, DisabledVaults: `[]`, Revision: 1, Status: "active"},
			friday + ":agent_friday": {ExternalID: "idcfg_friday", IdentityUUID: friday, AgentExternalID: "agent_friday", VaultMounts: `[]`, DisabledVaults: `[]`, Revision: 1, Status: "active"},
			friday + ":agent_shared": {ExternalID: "idcfg_shared", IdentityUUID: friday, AgentExternalID: "agent_shared", VaultMounts: `["vlt_shared"]`, DisabledVaults: `[]`, Revision: 1, Status: "active"},
		},
		pairings: map[string]db.ScopePairingRecord{
			"oc_left":   {ExternalID: "chsp_left", Status: "active", IdentityUUID: &left, AgentExternalID: "agent_left", Revision: 3, RemoteID: "oc_left", ScopeKind: "room"},
			"oc_right":  {ExternalID: "chsp_right", Status: "active", IdentityUUID: &right, AgentExternalID: "agent_right", Revision: 4, RemoteID: "oc_right", ScopeKind: "room"},
			"ou_alice":  {ExternalID: "chsp_alice", Status: "active", IdentityUUID: &friday, AgentExternalID: "agent_friday", Revision: 1, RemoteID: "ou_alice", ScopeKind: "direct"},
			"ou_bob":    {ExternalID: "chsp_bob", Status: "active", IdentityUUID: &friday, AgentExternalID: "agent_friday", Revision: 1, RemoteID: "ou_bob", ScopeKind: "direct"},
			"ou_shared": {ExternalID: "chsp_shared", Status: "active", IdentityUUID: &friday, AgentExternalID: "agent_shared", Revision: 1, RemoteID: "ou_shared", ScopeKind: "direct"},
		},
		vaults: map[string]db.Vault{
			"vlt_left": activeVault("vlt_left"), "vlt_right": activeVault("vlt_right"), "vlt_alice": activeVault("vlt_alice"), "vlt_bob": activeVault("vlt_bob"),
			"vlt_shared": {ExternalID: "vlt_shared", ArchivedAt: &now},
		},
		running: []string{"ses_running"},
	}
	return store
}

func activeVault(id string) db.Vault { return db.Vault{ExternalID: id, WorkspaceUUID: "ws"} }

func (s *acceptanceStore) FindExecutionContext(_ context.Context, _, channel string) (db.ExecutionContextRecord, error) {
	switch channel {
	case "chn_bot":
		return db.ExecutionContextRecord{Mode: "pairing", ChannelExternalID: channel}, nil
	case "chn_direct":
		return db.ExecutionContextRecord{Mode: "pairing", ChannelExternalID: channel}, nil
	case "chn_shared":
		return db.ExecutionContextRecord{Mode: "pairing", ChannelExternalID: channel}, nil
	default:
		return db.ExecutionContextRecord{}, db.ErrNotFound
	}
}

func (s *acceptanceStore) FindOpenScopePairing(_ context.Context, _, _, _, remote string) (db.ScopePairingRecord, error) {
	row, ok := s.pairings[remote]
	if !ok {
		return db.ScopePairingRecord{}, db.ErrNotFound
	}
	return row, nil
}

func (s *acceptanceStore) FindBusinessIdentityByUUID(_ context.Context, _, id string) (db.IdentityRecord, error) {
	row, ok := s.identities[id]
	if !ok {
		return db.IdentityRecord{}, db.ErrNotFound
	}
	return row, nil
}

func (s *acceptanceStore) FindBusinessIdentity(_ context.Context, _, external string) (db.IdentityRecord, error) {
	for _, row := range s.identities {
		if row.ExternalID == external && row.DeletedAt == nil {
			return row, nil
		}
	}
	return db.IdentityRecord{}, db.ErrNotFound
}

func (s *acceptanceStore) FindActiveIdentityConfig(_ context.Context, _, identity, agent string) (db.IdentityConfigRecord, error) {
	row, ok := s.configs[identity+":"+agent]
	if !ok {
		return db.IdentityConfigRecord{}, db.ErrNotFound
	}
	return row, nil
}

func (s *acceptanceStore) GetVault(_ context.Context, _, id string) (db.Vault, error) {
	if s.vaultErr != nil {
		return db.Vault{}, s.vaultErr
	}
	row, ok := s.vaults[id]
	if !ok {
		return db.Vault{}, db.ErrNotFound
	}
	return row, nil
}

func (s *acceptanceStore) GetAgent(_ context.Context, _, id string) (db.Agent, error) {
	return db.Agent{ExternalID: id, CurrentVersion: 1, Tools: []byte(`["bash"]`)}, nil
}

func (s *acceptanceStore) GetAgentVersion(_ context.Context, _, id string, version int) (db.Agent, error) {
	return db.Agent{ExternalID: id, CurrentVersion: version}, nil
}

func (s *acceptanceStore) FindSessionIdentity(_ context.Context, _, session string) (db.SessionIdentityRecord, error) {
	if session != "ses_live" {
		return db.SessionIdentityRecord{}, db.ErrNotFound
	}
	return db.SessionIdentityRecord{SessionExternalID: session, IdentityUUID: "id_friday", AgentExternalID: "agent_friday"}, nil
}

func (s *acceptanceStore) SetBusinessIdentityEnabled(_ context.Context, _, external string, enabled bool) error {
	for id, row := range s.identities {
		if row.ExternalID == external {
			row.Enabled = enabled
			s.identities[id] = row
			return nil
		}
	}
	return db.ErrNotFound
}

func (s *acceptanceStore) ListRunningIdentitySessions(context.Context, string, string) ([]string, error) {
	return append([]string{}, s.running...), nil
}

func (s *acceptanceStore) ListCleanupIdentityConfigs(ctx context.Context, workspace, identity, cursor string, limit int) ([]db.IdentityConfigRecord, error) {
	rows, err := s.ListIdentityConfigs(ctx, workspace, identity, "", cursor, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range s.configs {
		if row.IdentityUUID != identity || row.Status == "active" {
			continue
		}
		if cursor != "" && row.ExternalID >= cursor {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *acceptanceStore) CleanupEffects(context.Context, string, string) (db.CleanupEffectCounts, error) {
	return db.CleanupEffectCounts{ArchivedConfigs: 1, UnboundPairings: 1, ClosedEntries: 1}, nil
}

func (s *acceptanceStore) ListIdentityConfigs(_ context.Context, _, identity, agent, cursor string, limit int) ([]db.IdentityConfigRecord, error) {
	if s.listConfigErr != nil {
		return nil, s.listConfigErr
	}
	rows := make([]db.IdentityConfigRecord, 0, len(s.configs))
	for _, row := range s.configs {
		if row.IdentityUUID != identity || (row.Status != "" && row.Status != "active") {
			continue
		}
		if agent != "" && row.AgentExternalID != agent {
			continue
		}
		if cursor != "" && row.ExternalID >= cursor {
			continue
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(left, right db.IdentityConfigRecord) int {
		return strings.Compare(right.ExternalID, left.ExternalID)
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *acceptanceStore) ArchiveIdentityConfigs(context.Context, string, string) (int64, error) {
	if s.archiveErr != nil {
		return 0, s.archiveErr
	}
	return 1, nil
}

func (s *acceptanceStore) UnbindIdentityPairings(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (s *acceptanceStore) CloseFixedIdentityEntries(context.Context, string, string) (int64, error) {
	s.channelMutations++
	return 1, nil
}

func (s *acceptanceStore) CreateIdentityCleanupRun(_ context.Context, params db.InsertCleanupRunParams) (db.CleanupRunRecord, error) {
	return db.CleanupRunRecord{ExternalID: params.ExternalID, State: params.State}, nil
}

func (s *acceptanceStore) SaveIdentityCleanupRun(context.Context, string, string, string, string) error {
	return nil
}

func (s *acceptanceStore) CreateScopePairing(context.Context, db.InsertScopePairingParams) (db.ScopePairingRecord, error) {
	s.pairingCreates++
	return db.ScopePairingRecord{}, nil
}

func (s *acceptanceStore) CreateBusinessIdentity(context.Context, db.InsertIdentityParams) (db.IdentityRecord, error) {
	return db.IdentityRecord{}, db.ErrNotFound
}
func (s *acceptanceStore) ListBusinessIdentities(context.Context, db.ListIdentityParams) ([]db.IdentityRecord, error) {
	return nil, nil
}
func (s *acceptanceStore) UpdateBusinessIdentity(context.Context, db.UpdateIdentityParams) error {
	return nil
}
func (s *acceptanceStore) DeleteBusinessIdentity(context.Context, string, string) error {
	return nil
}
func (s *acceptanceStore) CreateIdentityConfig(context.Context, db.InsertIdentityConfigParams) (db.IdentityConfigRecord, error) {
	return db.IdentityConfigRecord{}, nil
}
func (s *acceptanceStore) UpdateIdentityConfig(context.Context, db.UpdateIdentityConfigParams) error {
	return nil
}
func (s *acceptanceStore) SetChannelConnectionStatus(context.Context, string, string, string) error {
	s.channelMutations++
	return nil
}
func (s *acceptanceStore) FindScopePairing(_ context.Context, _, externalID string) (db.ScopePairingRecord, error) {
	if s.activated != nil && s.activated.ExternalID == externalID {
		return *s.activated, nil
	}
	return db.ScopePairingRecord{}, db.ErrNotFound
}

func (s *acceptanceStore) FindPendingScopePairingByCode(_ context.Context, _, codeHash string, _ time.Time) (db.ScopePairingRecord, error) {
	if s.pendingHash == "" || s.pendingHash != codeHash {
		return db.ScopePairingRecord{}, db.ErrNotFound
	}
	return s.pendingCode, nil
}
func (s *acceptanceStore) ListScopePairings(context.Context, db.ListScopePairingParams) ([]db.ScopePairingRecord, error) {
	if s.rejectPairingList {
		return nil, db.ErrInvalidState
	}
	return nil, nil
}
func (s *acceptanceStore) ActivateScopePairingByCode(_ context.Context, params db.ActivateScopePairingParams) error {
	row := s.pendingCode
	row.Status = "active"
	row.ExternalID = params.ExternalID
	row.IdentityUUID = &params.IdentityUUID
	row.AgentExternalID = params.AgentExternalID
	s.activated = &row
	return nil
}
func (s *acceptanceStore) ActivateScopePairingByID(_ context.Context, params db.ActivateScopePairingParams) error {
	if s.activateErr != nil {
		if s.activated != nil {
			active := *s.activated
			active.Status = "active"
			active.IdentityUUID = &params.IdentityUUID
			active.AgentExternalID = params.AgentExternalID
			active.AgentVersion = params.AgentVersion
			s.activated = &active
		}
		return s.activateErr
	}
	return nil
}
func (s *acceptanceStore) RebindScopePairing(context.Context, db.RebindScopePairingParams) error {
	return nil
}
func (s *acceptanceStore) UnbindScopePairing(context.Context, string, string, int64) error {
	return nil
}
func (s *acceptanceStore) FindPersonalIdentityByActor(context.Context, string, string, string, string, string) (db.IdentityRecord, error) {
	return db.IdentityRecord{}, db.ErrNotFound
}
func (s *acceptanceStore) IsPersonalIdentity(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *acceptanceStore) ClaimPersonalIdentityByActor(context.Context, string, string, string, string, string, string) error {
	return nil
}
func (s *acceptanceStore) ListIdentitySessions(context.Context, string, string, int) ([]db.SessionIdentityRecord, error) {
	return nil, nil
}
func (s *acceptanceStore) BusinessIdentityStats(context.Context, string, string, *time.Time) (db.IdentityStatsRecord, error) {
	return db.IdentityStatsRecord{}, nil
}
func (s *acceptanceStore) ListBusinessIdentityAgents(context.Context, string, string, *time.Time, int) ([]db.IdentityAgentUsageRecord, error) {
	return nil, nil
}
func (s *acceptanceStore) ListAgentIdentityBindings(context.Context, string, string) ([]db.AgentBindingRecord, error) {
	return nil, nil
}
func (s *acceptanceStore) IsPersonalChannelVault(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *acceptanceStore) FindIdentityCleanupRun(context.Context, string, string) (db.CleanupRunRecord, error) {
	return db.CleanupRunRecord{}, db.ErrNotFound
}
func (s *acceptanceStore) GetSession(context.Context, string, string) (db.Session, bool, error) {
	return db.Session{}, false, nil
}

func TestCompleteByCodeLooksUpHash(t *testing.T) {
	store := newAcceptanceStore()
	code := "ABCDEFGH"
	store.pendingHash = HashPairingCode(code)
	store.pendingCode = db.ScopePairingRecord{ExternalID: "chsp_late", Status: "pending", ScopeKind: "room", RemoteID: "oc_late"}
	store.rejectPairingList = true
	view, err := NewService(store, nil).completeByCode(context.Background(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}, store.identities["id_friday"], PairingWrite{Code: code, AgentID: "agent_friday"})
	if err != nil {
		t.Fatal(err)
	}
	if view["id"] != "chsp_late" || view["status"] != "active" {
		t.Fatalf("pairing view: %#v", view)
	}
}

var _ Store = (*acceptanceStore)(nil)

func TestChannelSharedVaultsEnterIdentityCredentialScope(t *testing.T) {
	store := newAcceptanceStore()
	service := NewService(store, nil)
	ingress := Ingress{
		OrganizationUUID: "org", WorkspaceUUID: "ws", ChannelExternalID: "chn_direct", ChatType: "p2p", ActorID: "ou_alice",
		SharedVaultIDs: []string{"vlt_left", "vlt_right", "vlt_left"}, PersonalVaultID: "vlt_alice", PersonalActive: true,
	}
	decision, err := service.Resolve(t.Context(), ingress)
	if err != nil || strings.Join(decision.VaultIDs, ",") != "vlt_left,vlt_right,vlt_alice" {
		t.Fatalf("渠道共享挂载未进入有序凭据范围：%v %#v", err, decision)
	}
	store.vaults["vlt_right"] = db.Vault{ExternalID: "vlt_right", ArchivedAt: new(time.Now())}
	decision, err = service.Preview(t.Context(), ingress)
	if err != nil || decision.Mode != "hold" || len(decision.VaultIDs) != 0 {
		t.Fatalf("共享资源不可用时回退到了个人凭据：%v %#v", err, decision)
	}
}

func (s *acceptanceStore) GetChannelAgentEnvironment(context.Context, string, string, string) (string, error) {
	return "env_configured", s.pairingEnvironmentErr
}

func TestPairingWriteRequiresAgentEnvironment(t *testing.T) {
	store := newAcceptanceStore()
	service := NewService(store, nil)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	store.pairingEnvironmentErr = db.ErrNotFound
	store.activated = new(store.pairings["ou_alice"])
	_, err := service.CompletePairing(t.Context(), principal, PairingWrite{
		PairingID: "chsp_alice", IdentityID: "idn_friday", AgentID: "agent_friday",
	})
	if public, ok := errors.AsType[*apperr.Error](err); !ok || public.Kind != apperr.Conflict || !strings.Contains(public.PublicMessage, "渠道执行环境") {
		t.Fatalf("缺少执行环境时应拒绝配对：%v", err)
	}
	_, err = service.UpdatePairing(t.Context(), principal, "chsp_alice", PairingWrite{AgentID: "agent_friday", Revision: 1})
	if public, ok := errors.AsType[*apperr.Error](err); !ok || public.Kind != apperr.Conflict || !strings.Contains(public.PublicMessage, "渠道执行环境") {
		t.Fatalf("缺少执行环境时应拒绝改绑：%v", err)
	}
	store.pairingEnvironmentErr = nil
	version := 0
	_, err = service.CompletePairing(t.Context(), principal, PairingWrite{PairingID: "chsp_alice", IdentityID: "idn_friday", AgentID: "agent_friday", AgentVersion: &version})
	if public, ok := errors.AsType[*apperr.Error](err); !ok || public.Kind != apperr.InvalidArgument {
		t.Fatalf("版本 0 应被拒绝：%v", err)
	}
	pending := store.pairings["ou_alice"]
	pending.Status = "pending"
	store.activated = &pending
	store.activateErr = db.ErrInvalidState
	completed, err := service.CompletePairing(t.Context(), principal, PairingWrite{PairingID: "chsp_alice", IdentityID: "idn_friday", AgentID: "agent_friday"})
	if err != nil || completed["status"] != "active" {
		t.Fatalf("相同目标的并发激活应返回已有绑定：%v %#v", err, completed)
	}
}
