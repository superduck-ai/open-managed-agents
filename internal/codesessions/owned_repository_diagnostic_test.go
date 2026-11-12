package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
)

func TestOwnedRepositoryDiagnosticTypedRequestedVersions(t *testing.T) {
	raw := json.RawMessage(`{"runtime_skills":[{"name":"local","description":"LOCAL_DESCRIPTION_CANARY","version":"1","path":"/owned/local/SKILL.md","source":"local","skill_id":"skill_local","version_uuid":"version_local","sha256":"digest","requested_versions":["latest","1","latest"]}],"runtime_tools":["Bash","Read"]}`)
	got, ok := ownedRepoCatalogue(raw)
	if !ok {
		t.Fatal("typed requested_versions rejected")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("local JSON")
	}
	canonical, err := jsoncanonical.Canonicalize(fields["runtime_skills"])
	if err != nil || got["runtime_skills_sha256"] != ownedRepoHash(string(canonical)) {
		t.Fatal("full actual metadata hash lost")
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "LOCAL_DESCRIPTION_CANARY") || strings.Contains(string(encoded), `"requested_versions":[`) {
		t.Fatal("plaintext metadata leaked")
	}
	changed := strings.Replace(string(raw), `["latest","1","latest"]`, `["1","latest","latest"]`, 1)
	other, ok := ownedRepoCatalogue([]byte(changed))
	if !ok || other["runtime_skills_sha256"] == got["runtime_skills_sha256"] {
		t.Fatal("array order discarded")
	}
	for _, replacement := range []string{`null`, `"latest"`, `["latest",1]`, `[` + strings.Repeat(`"latest",`, 64) + `"latest"]`} {
		bad := strings.Replace(string(raw), `["latest","1","latest"]`, replacement, 1)
		if _, ok := ownedRepoCatalogue([]byte(bad)); ok {
			t.Fatal("malformed/unbounded requested_versions accepted")
		}
	}
	bad := strings.Replace(string(raw), `"requested_versions":`, `"unreviewed_metadata":`, 1)
	if _, ok := ownedRepoCatalogue([]byte(bad)); ok {
		t.Fatal("unknown field silently dropped")
	}
	t.Setenv(ownedRepoCaptureEnv, "")
	(dshLifecycle{}).ObserveOwnedRepositoryRegistration(context.Background(), db.DSHScope{}, raw, dsh.RegistrationIssue{})
}

func TestOwnedRepositoryScopeAndSinkFailClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "scope.json")
	valid := ownedRepoCaptureScope{RunNonce: strings.Repeat("a", 24), RepositoryURLSHA256: strings.Repeat("b", 64), ExpiresAt: time.Now().Add(time.Minute), Admissions: map[string]ownedRepoAdmission{strings.Repeat("c", 32): {AgentID: "agent_owned", AgentVersion: 1, AgentRunID: strings.Repeat("a", 24), MountSHA256: strings.Repeat("d", 64), CheckoutType: "commit", CheckoutSHA: strings.Repeat("e", 40)}}}
	t.Setenv(ownedRepoCaptureEnv, file)
	save := func(value ownedRepoCaptureScope) {
		raw, _ := json.Marshal(value)
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"expired", "long-expiry", "wrong-run", "two-admissions", "bad-commit", "public-file", "symlink", "oversize"} {
		copyRaw, _ := json.Marshal(valid)
		var scope ownedRepoCaptureScope
		_ = json.Unmarshal(copyRaw, &scope)
		switch mode {
		case "expired":
			scope.ExpiresAt = time.Now().Add(-time.Second)
		case "long-expiry":
			scope.ExpiresAt = time.Now().Add(time.Hour)
		case "wrong-run":
			scope.RunNonce = "foreign-run-nonce"
		case "two-admissions":
			scope.Admissions["another"] = ownedRepoAdmission{}
		case "bad-commit":
			a := scope.Admissions[strings.Repeat("c", 32)]
			a.CheckoutSHA = "branch"
			scope.Admissions[strings.Repeat("c", 32)] = a
		}
		save(scope)
		if mode == "public-file" {
			if err := os.Chmod(file, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "oversize" {
			if err := os.WriteFile(file, bytes.Repeat([]byte("x"), 8193), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "symlink" {
			target := filepath.Join(root, "actual")
			if err := os.Rename(file, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, file); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, ok := ownedRepoLoadScope(); ok {
			t.Fatal("unsafe scope admitted", mode)
		}
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	save(valid)
	if _, _, ok := ownedRepoLoadScope(); !ok {
		t.Fatal("actual private valid scope rejected")
	}
	sink := filepath.Join(root, "native.jsonl")
	if !ownedRepoAppend(sink, map[string]any{"event": "local_control"}) {
		t.Fatal("private bounded sink rejected")
	}
	for _, prior := range [][]byte{[]byte("unterminated"), []byte(strings.Repeat("{}\n", ownedRepoMaxRows)), bytes.Repeat([]byte("x"), ownedRepoMaxBytes)} {
		if err := os.WriteFile(sink, prior, 0600); err != nil {
			t.Fatal(err)
		}
		if ownedRepoAppend(sink, map[string]any{"event": "local_control"}) {
			t.Fatal("capped or partial source extended")
		}
		after, err := os.ReadFile(sink)
		if err != nil || !bytes.Equal(after, prior) {
			t.Fatal("invalid prior source mutated", err)
		}
	}
	if ownedRepoAppend(sink, map[string]any{"oversized": strings.Repeat("x", 96<<10)}) {
		t.Fatal("oversized row accepted")
	}
	t.Setenv(ownedRepoCaptureEnv, "")
	ctx := context.Background()
	if ownedRepoNativeRequestContext(ctx) != ctx {
		t.Fatal("default-off request context changed")
	}
}
