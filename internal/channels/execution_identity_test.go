package channels

import (
	"context"
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

func TestIdentityHistoryRejectsHeldAndStaleMount(t *testing.T) {
	worker := &channelWorker{supervisor: &runtimeSupervisor{ingress: holdIngress{}}}
	msg := &channelMessage{ChatType: "p2p", ChatID: "user", UserID: "user"}
	if worker.identitySessionOK(context.Background(), msg, "chat:user", omaSession{}) {
		t.Fatal("held execution stayed recoverable")
	}
	decision := identities.Decision{
		Mode: "execute", AgentExternalID: "agent_a", IdentityExternalID: "idn_a", BindingRevision: 2, ConfigRevision: 3,
		PersonalVaultID: "vlt_new", VaultIDs: []string{"vlt_new"}, ScopeKey: "chat:user",
	}
	worker.supervisor.ingress = fixedIngress{decision: decision}
	worker.supervisor.agentEnvironment = func(context.Context, string, string, string) (string, error) {
		return "env_1", nil
	}
	current := omaSession{
		Agent: omaSessionAgent{ID: "agent_a"}, EnvironmentID: "env_1", VaultIDs: []string{"vlt_new"},
		Metadata: map[string]string{"identity_id": "idn_a", "binding_revision": "2", "config_revision": "3", "personal_vault_id": "vlt_new", "scope_id": "chat:user"},
	}
	if !worker.identitySessionOK(context.Background(), msg, "chat:user", current) {
		t.Fatal("current mount was rejected")
	}
	current.Metadata["personal_vault_id"] = "vlt_old"
	if worker.identitySessionOK(context.Background(), msg, "chat:user", current) {
		t.Fatal("old vault was accepted")
	}
	worker.supervisor.ingress = nil
	if !worker.identitySessionOK(context.Background(), msg, "chat:user", current) {
		t.Fatal("legacy session was rejected")
	}
}

func TestResumeUsesAgentEnvironmentAndPreviewSkipsVaultCreate(t *testing.T) {
	decision := identities.Decision{
		Mode: "execute", AgentExternalID: "agent_a", IdentityExternalID: "idn_a", BindingRevision: 2, ConfigRevision: 3,
		PersonalVaultID: "vlt_new", VaultIDs: []string{"vlt_new"}, ScopeKey: "chat:user",
	}
	created := false
	worker := &channelWorker{supervisor: &runtimeSupervisor{
		ingress: fixedIngress{decision: decision},
		agentEnvironment: func(context.Context, string, string, string) (string, error) {
			return "env_current", nil
		},
		personalVault: func(context.Context, db.ChannelRecord, string) (string, error) {
			created = true
			return "vlt_created", nil
		},
		personalVaultLookup: func(context.Context, db.ChannelRecord, string) (string, bool, error) {
			return "vlt_existing", false, nil
		},
	}}
	msg := &channelMessage{ChatType: "p2p", ChatID: "user", UserID: "user"}
	session := omaSession{
		Agent: omaSessionAgent{ID: "agent_a"}, EnvironmentID: "env_old", VaultIDs: []string{"vlt_new"},
		Metadata: map[string]string{"identity_id": "idn_a", "binding_revision": "2", "config_revision": "3", "personal_vault_id": "vlt_new", "scope_id": "chat:user"},
	}
	if worker.identitySessionOK(context.Background(), msg, "chat:user", session) {
		t.Fatal("stale environment stayed recoverable")
	}
	if created {
		t.Fatal("preview created a personal vault")
	}
	session.EnvironmentID = "env_current"
	if !worker.identitySessionOK(context.Background(), msg, "chat:user", session) {
		t.Fatal("current environment was rejected")
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

type holdIngress struct{}

func (holdIngress) Resolve(context.Context, identities.Ingress) (identities.Decision, error) {
	return identities.Decision{Mode: "hold", Notice: "held"}, nil
}
func (holdIngress) Preview(context.Context, identities.Ingress) (identities.Decision, error) {
	return identities.Decision{Mode: "hold", Notice: "held"}, nil
}
func (holdIngress) BindSession(context.Context, identities.SessionBind) error { return nil }
func (holdIngress) ObserveConnection(context.Context, string, string, string) error {
	return nil
}
func (holdIngress) ExecutionView(context.Context, string, string) (map[string]any, error) {
	return nil, nil
}
func (holdIngress) SaveChannelContext(context.Context, auth.Principal, identities.ChannelContextInput) error {
	return nil
}

type fixedIngress struct{ decision identities.Decision }

func (f fixedIngress) Resolve(context.Context, identities.Ingress) (identities.Decision, error) {
	return f.decision, nil
}
func (f fixedIngress) Preview(context.Context, identities.Ingress) (identities.Decision, error) {
	return f.decision, nil
}
func (fixedIngress) BindSession(context.Context, identities.SessionBind) error { return nil }
func (fixedIngress) ObserveConnection(context.Context, string, string, string) error {
	return nil
}
func (fixedIngress) ExecutionView(context.Context, string, string) (map[string]any, error) {
	return nil, nil
}
func (fixedIngress) SaveChannelContext(context.Context, auth.Principal, identities.ChannelContextInput) error {
	return nil
}
