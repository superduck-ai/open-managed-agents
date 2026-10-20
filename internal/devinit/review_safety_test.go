package devinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestRegistryRetainsPathsWithStatErrors(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root)
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(file, "worktree")
	binding, _, err := registry.Create(invalid, "/init.yaml", "protected")
	if err != nil {
		t.Fatal(err)
	}
	orphans, err := registry.Orphans()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("stat error became an orphan: %v", orphans)
	}
	binding.Worktree = root
	if _, err := registry.Ensure(binding, "/init.yaml"); err == nil {
		t.Fatal("stat error allowed environment takeover")
	}
	if pathExists(filepath.Join(root, "missing")) {
		t.Fatal("missing path was considered present")
	}
}

func TestMissingCredentialsRejectsReservedIdentityAfterPhaseFallback(t *testing.T) {
	for _, phase := range []string{phasePendingSecrets, phaseInfra, phaseMigrated, phaseComplete} {
		for _, reserved := range []string{"organization", "user", "workspace", "keys", "none"} {
			t.Run(phase+"/"+reserved, func(t *testing.T) {
				root := t.TempDir()
				registry := newTestRegistry(t, root)
				binding, _, err := registry.Create(root, "/init.yaml", "test")
				if err != nil {
					t.Fatal(err)
				}
				layout, err := NewLayout(binding, root)
				if err != nil {
					t.Fatal(err)
				}
				if err := SaveBinding(layout, binding); err != nil {
					t.Fatal(err)
				}
				state := State{EnvID: binding.EnvID, Phase: phase}
				if reserved == "organization" {
					state.Organization = &OrganizationState{UUID: "reserved"}
				}
				if reserved == "user" {
					state.User = &UserState{UUID: "reserved"}
				}
				if reserved == "workspace" {
					state.Workspaces = []WorkspaceState{{UUID: "reserved"}}
				}
				if reserved == "keys" {
					state.Workspaces = []WorkspaceState{{APIKeys: []APIKeyState{{ExternalID: "apikey_reserved"}}}}
				}
				if err := SaveState(layout, state); err != nil {
					t.Fatal(err)
				}
				_, err = loadRunState(root, "/init.yaml", config.Config{}, Spec{}, ModelProvidersSpec{}, func() {})
				reject := reserved != "none" || phase == phaseComplete
				if (err != nil) != reject {
					t.Fatalf("reject=%v, got %v", reject, err)
				}
				if err != nil && !strings.Contains(err.Error(), "credentials file") {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTeardownRejectsCopiedBinding(t *testing.T) {
	for _, mismatch := range []string{"registry", "binding", "none"} {
		t.Run(mismatch, func(t *testing.T) {
			root := t.TempDir()
			registry := newTestRegistry(t, root)
			binding, _, err := registry.Create(root, "/init.yaml", "test")
			if err != nil {
				t.Fatal(err)
			}
			if mismatch == "registry" {
				if err := registry.withLock(func(entries *envRegistry) error {
					entry := entries.Envs[binding.Name]
					entry.Worktree = t.TempDir()
					entries.Envs[binding.Name] = entry
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if mismatch == "binding" {
				binding.Worktree = t.TempDir()
			}
			layout, err := NewLayout(binding, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := SaveBinding(layout, binding); err != nil {
				t.Fatal(err)
			}
			targets, err := teardownTargets(root, registry, TeardownOptions{Yes: true})
			if mismatch == "none" {
				if err != nil || len(targets) != 1 {
					t.Fatalf("matching teardown: %v, %v", targets, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "current worktree") {
				t.Fatalf("copied binding accepted: %v", err)
			}
		})
	}
}

func TestComposeUpFailuresExhaustRetries(t *testing.T) {
	root := t.TempDir()
	deploy := filepath.Join(root, "deploy", "docker-compose")
	if err := os.MkdirAll(deploy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deploy, "e2b-local.yaml"), []byte("server:\n  addr: \"0.0.0.0:3099\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(root, "attempts")
	script := fmt.Sprintf("#!/bin/sh\ncase \" $* \" in\n*' up --detach '*) echo attempt >> %q; echo 'port is already allocated' >&2; exit 1;;\n*) exit 0;;\nesac\n", calls)
	if err := os.WriteFile(calls, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := reconcileStack(t.Context(), root, EnvBinding{Name: "test", EnvID: "test"}, nil)
	if err == nil || !strings.Contains(err.Error(), "port is already allocated") {
		t.Fatalf("compose error: %v", err)
	}
	output, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(output), "attempt") != stackReconcileAttempts {
		t.Fatalf("attempts=%q: %v", output, err)
	}
}
