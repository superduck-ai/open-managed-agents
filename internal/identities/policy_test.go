package identities

import (
	"strings"
	"testing"
	"time"
)

func TestTwoActorsKeepSeparatePersonalVaults(t *testing.T) {
	left, err := PlanCredentials(CredentialInput{ChatType: "p2p", PersonalVault: "vlt_alice", PersonalActive: true})
	if err != nil {
		t.Fatal(err)
	}
	right, err := PlanCredentials(CredentialInput{ChatType: "p2p", PersonalVault: "vlt_bob", PersonalActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if left.PersonalVaultID == right.PersonalVaultID || left.VaultIDs[0] == right.VaultIDs[0] {
		t.Fatalf("actors borrowed credentials: %#v %#v", left, right)
	}
	group, err := PlanCredentials(CredentialInput{ChatType: "group", PersonalVault: "vlt_alice", PersonalActive: true, SharedMounts: []string{"vlt_shared"}, Availability: map[string]VaultState{"vlt_shared": VaultActive}})
	if err != nil {
		t.Fatal(err)
	}
	if len(group.VaultIDs) != 1 || group.VaultIDs[0] != "vlt_shared" || group.PersonalVaultID != "" {
		t.Fatalf("group mounted a personal vault: %#v", group)
	}
}

func TestSharedVaultFailureDoesNotFallBack(t *testing.T) {
	_, err := PlanCredentials(CredentialInput{
		ChatType: "p2p", SharedMounts: []string{"vlt_shared"}, Availability: map[string]VaultState{"vlt_shared": VaultArchived},
		PersonalVault: "vlt_personal", PersonalActive: true,
	})
	if err == nil || !strings.Contains(err.Error(), "shared credential") {
		t.Fatalf("expected shared failure, got %v", err)
	}
}

func TestRecreatedVaultRejectsOldSession(t *testing.T) {
	binding := SessionBinding{IdentityID: "idn_a", AgentID: "agent_a", BindingRevision: 1, ConfigRevision: 2, PersonalVaultID: "vlt_old", ScopeKey: "chat:user"}
	current := EntryState{IdentityID: "idn_a", AgentID: "agent_a", BindingRevision: 1, ConfigRevision: 2, PersonalVaultID: "vlt_new", ScopeKey: "chat:user", IdentityEnabled: true}
	if SessionCurrent(binding, current) {
		t.Fatal("old vault remained current")
	}
	current.PersonalVaultID = "vlt_old"
	if !SessionCurrent(binding, current) {
		t.Fatal("matching vault was rejected")
	}
	kept := FilterCurrentSessions([]SessionBinding{binding, {IdentityID: "idn_a", AgentID: "agent_a", PersonalVaultID: "vlt_other"}}, current)
	if len(kept) != 1 || kept[0].PersonalVaultID != "vlt_old" {
		t.Fatalf("history filter: %#v", kept)
	}
}

func TestOAuthResumeKeepsCallAndSkipsCompletedBash(t *testing.T) {
	resume := ResumeOriginalCall(ToolWait{SessionID: "ses_1", VaultID: "vlt_1", CallID: "tool_oauth", PendingToolIDs: []string{"bash_done", "tool_oauth", "tool_next"}, CompletedIDs: []string{"bash_done"}})
	if resume.SessionID != "ses_1" || resume.VaultID != "vlt_1" || resume.CallID != "tool_oauth" {
		t.Fatalf("call changed: %#v", resume)
	}
	if len(resume.ReplayToolIDs) != 1 || resume.ReplayToolIDs[0] != "tool_next" {
		t.Fatalf("bash was replayed: %#v", resume.ReplayToolIDs)
	}
	if UnknownDeliveryResend() {
		t.Fatal("unknown delivery was resent")
	}
	if AllowlistGrantsIdentity() {
		t.Fatal("allowlist granted an identity")
	}
}

func TestPairingExpiryRetryAndRevision(t *testing.T) {
	code, err := NewPairingCode()
	if err != nil || len(code) != 8 {
		t.Fatal(err, code)
	}
	pending := Pairing{Status: "pending", CodeHash: HashPairingCode(code), CodeExpires: time.Now().Add(time.Minute), Revision: 0}
	if _, err := ActivateWithCode(pending, code, time.Now().Add(11*time.Minute), "idn_a", "agent_a", nil); err == nil {
		t.Fatal("expired code activated")
	}
	active, changed, err := ActivateWithID(pending, "idn_a", "agent_a", nil)
	if err != nil || !changed || active.Revision != 1 {
		t.Fatal(err, changed, active.Revision)
	}
	again, changed, err := ActivateWithID(active, "idn_a", "agent_a", nil)
	if err != nil || changed || again.Revision != 1 {
		t.Fatal("same target retried as a new activation", changed, again.Revision)
	}
	unbound, changed, err := Unbind(active, active.Revision)
	if err != nil || !changed || unbound.Status != "unbound" {
		t.Fatal(err, unbound.Status)
	}
	if _, _, err := ActivateWithID(unbound, "idn_a", "agent_a", nil); err == nil {
		t.Fatal("unbound pairing restored")
	}
	if _, _, err := Rebind(active, active.Revision-1, "idn_b", "agent_b", nil); err == nil {
		t.Fatal("stale revision rebound")
	}
	rebound, changed, err := Rebind(active, active.Revision, "idn_b", "agent_b", nil)
	if err != nil || !changed || rebound.Revision != 2 || rebound.IdentityID != "idn_b" {
		t.Fatalf("rebind: %v %#v", err, rebound)
	}
}

func TestConfigOverrideDisableAndInheritance(t *testing.T) {
	base := AgentBase{Version: 3, Env: map[string]string{"REGION": "cn", "API_TOKEN": "secret"}, Vaults: []string{"vlt_agent"}, Tools: []string{"bash"}}
	current := ConfigState{Env: map[string]string{"REGION": "us"}, Vaults: []string{"vlt_shared"}, Disabled: []string{}}
	cleared := "REGION"
	next, err := MergeConfig(current, ConfigPatch{EnvSet: true, Env: map[string]*string{"REGION": nil, "TEAM": &cleared}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.Env["REGION"]; ok || next.Env["TEAM"] != "REGION" {
		t.Fatalf("inheritance: %#v", next.Env)
	}
	disabled, err := MergeConfig(next, ConfigPatch{DisabledSet: true, Disabled: []string{"vlt_agent"}})
	if err != nil {
		t.Fatal(err)
	}
	effective := CompileEffective(base, disabled, 4, false)
	for _, item := range effective.Env {
		if item.Name == "API_TOKEN" || strings.Contains(strings.ToLower(item.Value), "secret") {
			t.Fatalf("secret leaked: %#v", item)
		}
	}
	if effective.Env[0].Name != "REGION" || effective.Env[0].Source != "agent" {
		t.Fatalf("region source: %#v", effective.Env)
	}
	for _, vault := range effective.Vaults {
		if vault.ID == "vlt_agent" {
			t.Fatal("disabled vault stayed effective")
		}
	}
	for _, tool := range effective.Tools {
		if tool.Source != "agent" || tool.Name != "bash" {
			t.Fatalf("tool widened: %#v", effective.Tools)
		}
	}
	if _, err := MergeConfig(ConfigState{}, ConfigPatch{EnvSet: true, Env: map[string]*string{"PASSWORD": &cleared}}); err == nil {
		t.Fatal("secret env accepted")
	}
}

func TestCleanupConflictPartialAndSharedRetention(t *testing.T) {
	if !CleanupBlocked(CleanupPreview{RunningSessions: []string{"ses_running"}}) {
		t.Fatal("running session was not a conflict")
	}
	partial := FinishCleanup(CleanupOutcome{Failures: []string{"archive identity config failed"}, RetainedSharedVaults: []string{"vlt_shared"}, PersonalVaultsDeleted: false})
	if partial.Completed || partial.PersonalVaultsDeleted || partial.RetainedSharedVaults[0] != "vlt_shared" {
		t.Fatalf("partial cleanup: %#v", partial)
	}
	done := FinishCleanup(CleanupOutcome{ArchivedConfigs: 1, UnboundPairings: 1, ClosedEntries: 1})
	if !done.Completed || done.PersonalVaultsDeleted {
		t.Fatalf("cleanup reported the wrong completion: %#v", done)
	}
	retried := MergeCleanupOutcome(CleanupOutcome{ArchivedConfigs: 1, UnboundPairings: 2, ClosedEntries: 1, RetainedSharedVaults: []string{"vlt_shared"}}, FinishCleanup(CleanupOutcome{Failures: []string{"list shared vaults failed"}}))
	if retried.Completed || retried.ArchivedConfigs != 1 || retried.UnboundPairings != 2 || len(retried.RetainedSharedVaults) != 1 || retried.RetainedSharedVaults[0] != "vlt_shared" {
		t.Fatalf("retry dropped retained cleanup progress: %#v", retried)
	}
}

func TestDisabledIdentityBlocksOnlyNewRounds(t *testing.T) {
	binding := SessionBinding{IdentityID: "idn_a", AgentID: "agent_a", ScopeKey: "chat:1"}
	if SessionCurrent(binding, EntryState{IdentityID: "idn_a", AgentID: "agent_a", ScopeKey: "chat:1", IdentityEnabled: false}) {
		t.Fatal("disabled identity stayed current")
	}
}
