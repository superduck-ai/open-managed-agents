package channels

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/identities"
)

func TestTopicsShareRoomKindAndIsolateSessionKeys(t *testing.T) {
	left := channelScopeKey(&channelMessage{ChatType: "group", ChatID: "oc_room", RootID: "om_topic_a"})
	right := channelScopeKey(&channelMessage{ChatType: "group", ChatID: "oc_room", RootID: "om_topic_b"})
	other := channelScopeKey(&channelMessage{ChatType: "group", ChatID: "oc_other", RootID: "om_topic_a"})
	if left == right || left == other {
		t.Fatalf("scope keys collided: %s %s %s", left, right, other)
	}
	if identities.ScopeKind("group") != "room" || identities.ScopeKind("group") != identities.ScopeKind("group") {
		t.Fatal("topics did not share the room pairing kind")
	}
	if identities.ScopeKind("p2p") == identities.ScopeKind("group") {
		t.Fatal("direct and room scopes collapsed")
	}
}

func TestResumeUsesEffectiveTargetAndReadsVaultOnce(t *testing.T) {
	decision := identities.Decision{Mode: "execute", AgentExternalID: "agent_paired", AgentVersion: 4, IdentityUUID: "identity-uuid",
		IdentityExternalID: "idn_a", BindingRevision: 2, ConfigRevision: 3, PersonalVaultID: "vlt_new", VaultIDs: []string{"vlt_new"}}
	store := &fakeRuntimeStore{identityBinding: db.SessionIdentityRecord{IdentityUUID: decision.IdentityUUID, ChannelExternalID: "chn_1",
		ScopeKey: "chat:user", BindingRevision: decision.BindingRevision, ConfigRevision: decision.ConfigRevision}}
	lookups := 0
	worker := &channelWorker{record: db.ChannelRecord{ExternalID: "chn_1", AgentExternalID: "agent_default", ChannelType: "feishu", AppID: "app"},
		supervisor: &runtimeSupervisor{
			store: store, ingress: fixedIngress{decision: decision},
			agentEnvironment: func(_ context.Context, _, _, agent string) (string, error) {
				if agent != "agent_paired" {
					t.Fatalf("恢复使用了默认 Agent：%s", agent)
				}
				return "env_current", nil
			},
			personalVault: func(context.Context, db.ChannelRecord, string) (string, error) {
				t.Fatal("读取历史创建了个人 Vault")
				return "", nil
			},
			personalVaultLookup: func(context.Context, db.ChannelRecord, string) (string, bool, error) {
				lookups++
				return "vlt_new", true, nil
			},
		},
	}
	msg := &channelMessage{ChatType: "p2p", ChatID: "user", UserID: "user"}
	target, err := worker.resolveRecoveryTarget(t.Context(), msg, "chat:user")
	if err != nil {
		t.Fatal(err)
	}
	session := omaSession{ID: "sesn_1", Status: "idle", Agent: omaSessionAgent{ID: "agent_paired", Version: 4}, EnvironmentID: "env_current", VaultIDs: []string{"vlt_new"},
		Metadata: map[string]string{"source": "feishu", "app_id": "app", "channel_id": "chn_1", "credential_binding_mode": "first_vault",
			"agent_version": "4", "identity_id": "idn_a", "binding_revision": "2", "config_revision": "3", "personal_vault_id": "vlt_new", "scope_id": "chat:user"},
	}
	if ok, err := worker.recoverableSession(t.Context(), session, "chat:user", "", target); err != nil || !ok || lookups != 1 {
		t.Fatal("当前配对会话不可恢复，或重复读取 Vault")
	}
	session.EnvironmentID = "env_old"
	if ok, err := worker.recoverableSession(t.Context(), session, "chat:user", "", target); err != nil || ok {
		t.Fatal("旧执行环境可恢复")
	}
	session.EnvironmentID = "env_current"
	worker.record.Mounts = `{"memory_store_id":"mem_new"}`
	if ok, err := worker.recoverableSession(t.Context(), session, "chat:user", "", target); err != nil || ok {
		t.Fatal("资源配置变化后复用了旧会话")
	}
	worker.record.Mounts = ""
	store.identityBinding.ConfigRevision = decision.ConfigRevision - 1
	if ok, err := worker.recoverableSession(t.Context(), session, "chat:user", "", target); err != nil || ok {
		t.Fatal("旧配置修订可恢复")
	}
	store.identityBinding.ConfigRevision = decision.ConfigRevision
	store.identityBinding = db.SessionIdentityRecord{}
	if ok, err := worker.recoverableSession(t.Context(), session, "chat:user", "", target); err != nil || ok {
		t.Fatal("无正式身份绑定的会话可恢复")
	}
	store.identityLookupErr = errors.New("identity lookup failed")
	if _, err := worker.recoverableSession(t.Context(), session, "chat:user", "", target); err == nil {
		t.Fatal("身份读取失败被当成不兼容")
	}
	worker.supervisor.ingress = holdIngress{}
	held, err := worker.resolveRecoveryTarget(t.Context(), msg, "chat:user")
	if err != nil || held.decision.Mode != "hold" {
		t.Fatalf("执行已拦截时还能恢复：%+v %v", held, err)
	}
}

func TestExecutionModeMapsFixedContextWithoutInventingIdentity(t *testing.T) {
	mode, present, err := executionMode(channelMutation{})
	if err != nil || present || mode != "" {
		t.Fatalf("legacy create changed: %s %v %v", mode, present, err)
	}
	mode, present, err = executionMode(channelMutation{IdentityResolution: []byte(`{"mode":"pairing"}`)})
	if err != nil || !present || mode != "pairing" {
		t.Fatalf("pairing mode: %s %v %v", mode, present, err)
	}
	id := "idn_service"
	mode, present, err = executionMode(channelMutation{IdentityID: &id})
	if err != nil || !present || mode != "fixed" {
		t.Fatalf("fixed facade: %s %v %v", mode, present, err)
	}
}

type inactiveSelfPairing struct{}

func (inactiveSelfPairing) PersonalActorIdentity(context.Context, identities.Ingress, bool) (db.IdentityRecord, error) {
	return db.IdentityRecord{}, db.ErrNotFound
}

func (inactiveSelfPairing) AllowsSelfPairing(context.Context, identities.Ingress) (bool, error) {
	return false, nil
}
func (inactiveSelfPairing) ConfirmSelfPairing(context.Context, identities.Ingress, string, *int) error {
	return db.ErrInvalidState
}

type holdIngress struct{ inactiveSelfPairing }

func (holdIngress) Resolve(context.Context, identities.Ingress) (identities.Decision, error) {
	return identities.Decision{Mode: "hold", Notice: "held"}, nil
}
func (holdIngress) Preview(context.Context, identities.Ingress) (identities.Decision, error) {
	return identities.Decision{Mode: "hold", Notice: "held"}, nil
}
func (holdIngress) ObserveConnection(context.Context, string, string, string) error {
	return nil
}
func (holdIngress) ExecutionView(context.Context, string, string) (map[string]any, error) {
	return nil, nil
}
func (holdIngress) ResolveChannelIdentity(context.Context, auth.Principal, string) (string, error) {
	return "", nil
}

type fixedIngress struct {
	inactiveSelfPairing
	decision identities.Decision
}

func (f fixedIngress) Resolve(context.Context, identities.Ingress) (identities.Decision, error) {
	return f.decision, nil
}
func (f fixedIngress) Preview(context.Context, identities.Ingress) (identities.Decision, error) {
	return f.decision, nil
}
func (fixedIngress) ObserveConnection(context.Context, string, string, string) error {
	return nil
}
func (fixedIngress) ExecutionView(context.Context, string, string) (map[string]any, error) {
	return nil, nil
}
func (fixedIngress) ResolveChannelIdentity(context.Context, auth.Principal, string) (string, error) {
	return "", nil
}
