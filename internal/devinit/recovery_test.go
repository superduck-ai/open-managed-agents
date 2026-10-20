package devinit

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReserveIdentityRejectsMissingCredential(t *testing.T) {
	state := State{}
	err := prepareResourceIdentities(Layout{StateFile: filepath.Join(t.TempDir(), "state.json")},
		Spec{Workspaces: []WorkspaceSpec{{Ref: "default", APIKeys: []APIKeySpec{{Ref: "key"}}}}}, &state, &Credentials{})
	if err == nil {
		t.Fatal("missing plaintext credential was accepted")
	}
}

func TestReservedIdentitiesSurviveStateReload(t *testing.T) {
	dir := t.TempDir()
	layout := Layout{StateFile: filepath.Join(dir, "state.json"), CredentialsFile: filepath.Join(dir, "credentials.json")}
	spec := Spec{OrganizationName: "OMA", UserEmail: "test@oma.ai", Workspaces: []WorkspaceSpec{
		{Ref: "default", IsDefault: true, APIKeys: []APIKeySpec{{Ref: "key-1"}, {Ref: "key-2"}}},
	}}
	state := State{EnvID: "test"}
	credentials := Credentials{EnvID: "test", BrainClaimToken: "token", APIKeys: []APICredential{
		{WorkspaceRef: "default", Ref: "key-1", Key: "first"},
		{WorkspaceRef: "default", Ref: "key-2", Key: "second"},
	}}
	if err := prepareResourceIdentities(layout, spec, &state, &credentials); err != nil {
		t.Fatal(err)
	}
	original := state
	reloaded, _, err := LoadState(layout.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	savedCredentials, _, err := LoadCredentials(layout.CredentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareResourceIdentities(layout, spec, &reloaded, &savedCredentials); err != nil {
		t.Fatal(err)
	}
	if reloaded.Organization.UUID != original.Organization.UUID || reloaded.User.ExternalID != original.User.ExternalID {
		t.Fatal("retry changed identity")
	}
	for i, key := range reloaded.Workspaces[0].APIKeys {
		if key.ExternalID != original.Workspaces[0].APIKeys[i].ExternalID || key.ExternalID != savedCredentials.APIKeys[i].ExternalID || !key.Pending {
			t.Fatal("retry lost the pending key identity")
		}
	}
}

func TestEnsureListenerPortsPreservesRunningListeners(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	runtime := RuntimePorts{APIPort: port, WebPort: port}
	if err := ensureListenerPorts(&runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.APIPort != port || runtime.WebPort != port {
		t.Fatal("initialization changed a running listener port")
	}
}

func TestRegistryResumesBeforeBindingWasSaved(t *testing.T) {
	repo := t.TempDir()
	registry := newTestRegistry(t, repo)
	first, _, err := registry.Create(repo, "/init.yaml", "name")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := registry.Create(repo, "/init.yaml", "name")
	if err != nil {
		t.Fatal(err)
	}
	if first.EnvID != second.EnvID || first.Name != second.Name {
		t.Fatal("retry allocated another environment")
	}
}

func TestStackRejectsForeignResourcesBeforeMutation(t *testing.T) {
	for _, kind := range []string{"container", "volume", "network"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\nif [ \"$1\" = \"" + kind + "\" ]; then\ncase \"$2\" in\nls) echo foreign;;\ninspect) echo '{\"oma.devinit.env_id\":\"other-environment\"}';;\n*) exit 99;;\nesac\nfi\n"
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			binding := EnvBinding{Name: "ownership-test", EnvID: "mine"}
			if _, err := EnsureStack(t.Context(), dir, binding, nil); err == nil || !strings.Contains(err.Error(), "is not owned") {
				t.Fatalf("foreign resource was not rejected by ownership: %v", err)
			}
			if err := TeardownStack(t.Context(), dir, binding); err == nil || !strings.Contains(err.Error(), "is not owned") {
				t.Fatalf("foreign resource deletion was not rejected by ownership: %v", err)
			}
		})
	}
}
