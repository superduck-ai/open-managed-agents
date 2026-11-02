package devinit

import (
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"os"
	"path/filepath"
	"testing"
)

func TestResetResourceRegistrationRejectsBackupFailure(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	prepared := preparedRun{layout: Layout{SecretsDir: blocked}, state: State{EnvID: "test", Organization: &OrganizationState{UUID: "old"}}}
	if err := resetResourceRegistration(&prepared, nil); err == nil {
		t.Fatal("accepted unwritable backup")
	}
	if prepared.state.Organization.UUID != "old" {
		t.Fatal("changed state before backup")
	}
}

func TestResetResourceRegistrationReservesNewResources(t *testing.T) {
	dir := t.TempDir()
	prepared := preparedRun{
		layout:      Layout{SecretsDir: dir, StateFile: filepath.Join(dir, "state.json"), CredentialsFile: filepath.Join(dir, "credentials.json")},
		spec:        Spec{OrganizationName: "OMA", Workspaces: []WorkspaceSpec{{Ref: "default", APIKeys: []APIKeySpec{{Ref: "key"}}}}},
		state:       State{EnvID: "test", Name: "env", Runtime: &RuntimePorts{APIPort: 40003}, Organization: &OrganizationState{UUID: "old"}, Providers: []ProviderState{{ExternalID: "old-provider"}}},
		credentials: Credentials{EnvID: "test", BrainClaimToken: "token", APIKeys: []APICredential{{WorkspaceRef: "default", Ref: "key", ExternalID: "old-key", Key: "old-secret"}}},
	}
	if err := resetResourceRegistration(&prepared, nil); err != nil {
		t.Fatal(err)
	}
	if prepared.state.Organization.UUID == "old" || !prepared.state.IdentityPending || len(prepared.state.Providers) != 0 {
		t.Fatal("stale registration retained")
	}
	if prepared.credentials.APIKeys[0].Key == "old-secret" || prepared.credentials.APIKeys[0].ExternalID == "old-key" {
		t.Fatal("stale API credential retained")
	}
	if prepared.credentials.BrainClaimToken != "token" || prepared.state.Runtime.APIPort != 40003 {
		t.Fatal("lost environment settings")
	}
	var backup struct {
		State       State
		Credentials Credentials
	}
	path := filepath.Join(dir, "update-recovery.json")
	if err := readJSONFile(path, &backup); err != nil {
		t.Fatal(err)
	}
	if backup.State.Organization.UUID != "old" || backup.Credentials.APIKeys[0].Key != "old-secret" {
		t.Fatal("lost backup")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal("backup is not private")
	}
	saved, _, err := LoadState(prepared.layout.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Organization.UUID != prepared.state.Organization.UUID || !saved.IdentityPending {
		t.Fatal("recovery is not resumable")
	}
}

func TestSelectRecoveryOrganization(t *testing.T) {
	for _, test := range []struct {
		name          string
		organizations []platform.UserOrganizationRecord
		wantError     bool
		wantUUID      string
	}{
		{name: "ambiguous", organizations: []platform.UserOrganizationRecord{{OrganizationRecord: platform.OrganizationRecord{UUID: "a", Name: "OMA"}, Role: "admin"}, {OrganizationRecord: platform.OrganizationRecord{UUID: "b", Name: "OMA"}, Role: "admin"}}, wantError: true},
		{name: "not admin", organizations: []platform.UserOrganizationRecord{{OrganizationRecord: platform.OrganizationRecord{UUID: "a", Name: "OMA"}, Role: "user"}}, wantError: true},
		{name: "absent"},
		{name: "unique", organizations: []platform.UserOrganizationRecord{{OrganizationRecord: platform.OrganizationRecord{UUID: "a", Name: "OMA"}, Role: "admin"}, {OrganizationRecord: platform.OrganizationRecord{UUID: "b", Name: "Other"}, Role: "admin"}}, wantUUID: "a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate, err := selectRecoveryOrganization(test.organizations, "OMA")
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.wantUUID != "" && (candidate == nil || candidate.UUID != test.wantUUID) {
				t.Fatal("wrong recovery organization")
			}
			if test.wantUUID == "" && candidate != nil {
				t.Fatal("unexpected recovery organization")
			}
		})
	}
}

func TestRecoveredIdentitiesAreNotWrittenAgain(t *testing.T) {
	dir := t.TempDir()
	prepared := preparedRun{
		layout:      Layout{SecretsDir: dir, StateFile: filepath.Join(dir, "state.json"), CredentialsFile: filepath.Join(dir, "credentials.json")},
		spec:        Spec{OrganizationName: "OMA", Workspaces: []WorkspaceSpec{{Ref: "default", APIKeys: []APIKeySpec{{Ref: "key"}}}}},
		state:       State{EnvID: "test", Organization: &OrganizationState{UUID: "old"}},
		credentials: Credentials{EnvID: "test", APIKeys: []APICredential{{WorkspaceRef: "default", Ref: "key", Key: "old-secret"}}},
	}
	if err := resetResourceRegistration(&prepared, nil); err != nil {
		t.Fatal(err)
	}
	savedState, err := os.ReadFile(prepared.layout.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	savedCredentials, err := os.ReadFile(prepared.layout.CredentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	prepared.layout.StateFile = filepath.Join(blocked, "state.json")
	prepared.layout.CredentialsFile = filepath.Join(blocked, "credentials.json")
	if err := prepared.prepareIdentities(); err != nil {
		t.Fatalf("repeated persistence after recovery: %v", err)
	}
	reloaded, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil || string(reloaded) != string(savedState) {
		t.Fatalf("changed recovered state: %v", err)
	}
	reloaded, err = os.ReadFile(filepath.Join(dir, "credentials.json"))
	if err != nil || string(reloaded) != string(savedCredentials) {
		t.Fatalf("changed recovered credentials: %v", err)
	}
}
