package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

type recordingIssuer struct {
	identity filestore.TokenIdentity
	readonly bool
}

func (issuer *recordingIssuer) Issue(identity filestore.TokenIdentity) (string, error) {
	issuer.identity = identity
	return "read-write-token", nil
}

func (issuer *recordingIssuer) IssueReadonly(identity filestore.TokenIdentity) (string, error) {
	issuer.identity = identity
	issuer.readonly = true
	return "read-only-token", nil
}

func TestRunRejectsIncompleteIdentity(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	loadCalled := false
	exitCode := run(nil, &stdout, &stderr, func(string) (tokenIssuer, error) {
		loadCalled = true
		return nil, errors.New("must not load")
	})
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if loadCalled {
		t.Fatal("credential loader was called for invalid arguments")
	}
	if !strings.Contains(stderr.String(), "--sub is required") {
		t.Fatalf("stderr = %q, want missing --sub error", stderr.String())
	}
}

func TestRunIssuesReadonlyToken(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	issuer := &recordingIssuer{}
	var loadedConfigPath string
	exitCode := run([]string{
		"--config", "test-config.yaml",
		"--sub", "user_test",
		"--org-uuid", "11111111-1111-4111-8111-111111111111",
		"--account-uuid", "22222222-2222-4222-8222-222222222222",
		"--workspace-uuid", "33333333-3333-4333-8333-333333333333",
		"--workspace-tagged-id", "wrkspc_test",
		"--filesystem-id", "fs_test",
		"--org-taint", "restricted",
		"--org-taint", "compliance",
		"--workspace-cmek-enabled",
		"--readonly",
	}, &stdout, &stderr, func(configPath string) (tokenIssuer, error) {
		loadedConfigPath = configPath
		return issuer, nil
	})
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if stdout.String() != "read-only-token\n" {
		t.Fatalf("stdout = %q, want token only", stdout.String())
	}
	if loadedConfigPath != "test-config.yaml" {
		t.Fatalf("config path = %q", loadedConfigPath)
	}
	if !issuer.readonly {
		t.Fatal("readonly issuer was not selected")
	}
	if issuer.identity.ResolvedWorkspaceTaggedID != "wrkspc_test" {
		t.Fatalf("resolved workspace tagged ID = %q", issuer.identity.ResolvedWorkspaceTaggedID)
	}
	if len(issuer.identity.OrgTaints) != 2 || issuer.identity.OrgTaints[0] != "restricted" || issuer.identity.OrgTaints[1] != "compliance" {
		t.Fatalf("organization taints = %#v", issuer.identity.OrgTaints)
	}
	if !issuer.identity.WorkspaceCMEKEnabled {
		t.Fatal("workspace CMEK flag was not propagated")
	}
}
