package sessionresource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestNormalizeGitRepositorySpecRejectsUnsafeInputs(t *testing.T) {
	cases := []struct{ name, url, mount, checkout string }{
		{"url credentials", `"https://token@github.com/owner/repo"`, "", ""},
		{"http", `"http://github.com/owner/repo"`, "", ""},
		{"query", `"https://github.com/owner/repo?x=1"`, "", ""},
		{"fragment", `"https://github.com/owner/repo#"`, "", ""},
		{"escaped path", `"https://github.com/owner/%72epo"`, "", ""},
		{"workspace root", "", `"/workspace"`, ""},
		{"outside workspace", "", `"/tmp/repo"`, ""},
		{"traversal", "", `"/workspace/../repo"`, ""},
		{"hidden runtime", "", `"/workspace/.claude/repo"`, ""},
		{"control", "", `"/workspace/repo\n"`, ""},
		{"windows path", "", `"/workspace/repo\\nested"`, ""},
		{"branch option", "", "", `{"type":"branch","name":"--upload-pack=evil"}`},
		{"branch reflog", "", "", `{"type":"branch","name":"main@{1}"}`},
		{"branch traversal", "", "", `{"type":"branch","name":"../main"}`},
		{"branch sha", "", "", `{"type":"branch","name":"main","sha":"aaa"}`},
		{"short sha", "", "", `{"type":"commit","sha":"123456"}`},
		{"checkout wrong type", "", "", `{"type":"tag","name":"v1"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.url == "" {
				test.url = `"https://github.com/owner/repo"`
			}
			if _, err := NormalizeGitRepositorySpec(json.RawMessage(test.url), json.RawMessage(test.mount), json.RawMessage(test.checkout)); err == nil {
				t.Fatal("invalid resource accepted")
			}
		})
	}
}

func TestGitRepositorySpecsRejectAmbiguousRepositoriesAndPaths(t *testing.T) {
	for _, specs := range [][]GitRepositorySpec{
		{{URL: "https://GITHUB.COM:443/owner/repo", MountPath: "/workspace/first"}, {URL: "https://github.com/owner/repo", MountPath: "/workspace/second"}},
		{{URL: "https://github.com/a/one", MountPath: "/workspace/repo"}, {URL: "https://github.com/a/two", MountPath: "/workspace/repo/child"}},
		{{URL: "https://github.com/a/one", MountPath: "/workspace/repo"}, {URL: "https://github.com/a/two", MountPath: "/workspace/repo"}},
	} {
		if err := ValidateGitRepositoryConflicts(specs); err == nil {
			t.Fatal("ambiguous resource set accepted")
		}
	}
}

func TestGitRepositoryCommitRequiresFullSHA(t *testing.T) {
	for _, test := range []struct {
		name string
		sha  string
	}{
		{"empty", ""},
		{"short", "abcdef0"},
		{"sha1 truncated", strings.Repeat("a", 39)},
		{"sha1 extended", strings.Repeat("a", 41)},
		{"sha256 truncated", strings.Repeat("a", 63)},
		{"sha256 extended", strings.Repeat("a", 65)},
		{"non hexadecimal", strings.Repeat("g", 40)},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkout := json.RawMessage(`{"type":"commit","sha":"` + test.sha + `"}`)
			if _, err := NormalizeGitRepositorySpec(json.RawMessage(`"https://github.com/owner/repo"`), nil, checkout); err == nil {
				t.Fatal("invalid commit SHA accepted at resource input")
			}
			stored := json.RawMessage(`{"url":"https://github.com/owner/repo","mount_path":"/workspace/repo","checkout":` + string(checkout) + `}`)
			if _, err := ParseStoredGitRepositorySpec(stored); err == nil {
				t.Fatal("invalid persisted commit SHA accepted")
			}
		})
	}
	for _, length := range []int{40, 64} {
		sha := strings.Repeat("A", length)
		checkout := json.RawMessage(`{"type":"commit","sha":"` + sha + `"}`)
		spec, err := NormalizeGitRepositorySpec(json.RawMessage(`"https://github.com/owner/repo"`), nil, checkout)
		if err != nil || spec.Checkout == nil || spec.Checkout.SHA != strings.ToLower(sha) {
			t.Fatalf("full %d-character SHA normalization: spec=%+v err=%v", length, spec, err)
		}
		stored := json.RawMessage(`{"url":"https://github.com/owner/repo","mount_path":"/workspace/repo","checkout":` + string(checkout) + `}`)
		spec, err = ParseStoredGitRepositorySpec(stored)
		if err != nil || spec.Checkout == nil || spec.Checkout.SHA != strings.ToLower(sha) {
			t.Fatalf("full %d-character persisted SHA normalization: spec=%+v err=%v", length, spec, err)
		}
	}
}

func TestNormalizeGitRepositorySpecDefaultsAndCheckout(t *testing.T) {
	for _, checkout := range []string{"", "null", `{"type":"branch","name":"feature/git-resources"}`, `{"type":"commit","sha":"` + strings.Repeat("A", 40) + `"}`} {
		spec, err := NormalizeGitRepositorySpec(json.RawMessage(`"https://github.com/owner/repo"`), nil, json.RawMessage(checkout))
		if err != nil || spec.MountPath != "/workspace/repo" {
			t.Fatalf("spec = %+v, err = %v", spec, err)
		}
		payload, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseStoredGitRepositorySpec(payload); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := NormalizeGitRepositorySpec(json.RawMessage(`"https://github.com/owner/.github"`), nil, nil)
	if err != nil || spec.MountPath != "/workspace/.github" {
		t.Fatalf("dot-prefixed repository default = %+v, %v", spec, err)
	}
	if !validGitBranch(strings.Repeat("汉", 255)) || validGitBranch(strings.Repeat("汉", 256)) {
		t.Fatal("branch length must count characters")
	}
	if _, err := ParseStoredGitRepositorySpec(json.RawMessage(`{"url":"https://github.com/owner/repo"}`)); err == nil {
		t.Fatal("stored missing path received a default")
	}
}

func TestGitRepositoryTokenEnvelopeRejectsPlaintextAndDifferentWorkspaces(t *testing.T) {
	ctx := context.Background()
	service, err := secrets.NewLocalService(ctx, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	binding := secrets.ResourceBinding{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	if _, err := DecryptGitToken(ctx, service, binding, json.RawMessage(`{"authorization_token":"legacy"}`)); err == nil {
		t.Fatal("plaintext token accepted")
	}
	raw, err := EncryptGitToken(ctx, service, binding, "github-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "github-test-secret") || strings.Contains(string(raw), "authorization_token") {
		t.Fatal("plaintext token persisted")
	}
	wrong := binding
	wrong.WorkspaceUUID = "ws_other"
	if _, err := DecryptGitToken(ctx, service, wrong, raw); err == nil {
		t.Fatal("cross-workspace token accepted")
	}
	token, err := DecryptGitToken(ctx, service, binding, raw)
	if err != nil || token != "github-test-secret" {
		t.Fatalf("token decrypt failed: %v", err)
	}
	for _, raw := range []string{`123`, `{}`, `"token\r\ninjected"`, `" token "`} {
		if _, err := ParseGitTokenInput(json.RawMessage(raw)); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}

func TestAnonymousGitRepositoryToken(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`""`)} {
		token, err := ParseGitTokenInput(raw)
		if err != nil || token != "" {
			t.Fatalf("parse anonymous: %q %v", token, err)
		}
		stored, err := EncryptGitToken(context.Background(), nil, secrets.ResourceBinding{}, token)
		if err != nil || string(stored) != "null" {
			t.Fatalf("seal anonymous: %s %v", stored, err)
		}
		token, err = DecryptGitToken(context.Background(), nil, secrets.ResourceBinding{}, stored)
		if err != nil || token != "" {
			t.Fatalf("open anonymous: %q %v", token, err)
		}
	}
}

func TestAnonymousGitRepositoryTokenFromSQLNull(t *testing.T) {
	token, err := DecryptGitToken(context.Background(), nil, secrets.ResourceBinding{}, nil)
	if err != nil || token != "" {
		t.Fatalf("SQL NULL credential: %q %v", token, err)
	}
}

func TestNormalizeSelfHostedGitRepository(t *testing.T) {
	for _, repositoryURL := range []string{
		"https://git.internal/group/subgroup/repo.git",
		"https://git.internal:443/group/subgroup/repo.git/",
		"https://192.168.1.20/team/repo",
		"https://github.com/owner/repo.git",
	} {
		raw, _ := json.Marshal(repositoryURL)
		spec, err := NormalizeGitRepositorySpec(raw, nil, nil)
		if err != nil || spec.URL != repositoryURL || spec.MountPath != "/workspace/repo" {
			t.Fatalf("repository %s: spec=%+v err=%v", repositoryURL, spec, err)
		}
	}
	for _, repositoryURL := range []string{"https://git.internal:8443/group/repo", "https://git.internal", "https://git.internal/", "https://git.internal/team/../repo", "https://git.internal/team//repo"} {
		if err := ValidateGitRepositoryURL(repositoryURL); err == nil {
			t.Fatalf("ambiguous URL accepted: %s", repositoryURL)
		}
	}
	if err := ValidateGitRepositoryConflicts([]GitRepositorySpec{
		{URL: "https://GIT.internal:443/group/repo/", MountPath: "/workspace/one"},
		{URL: "https://git.internal/group/repo", MountPath: "/workspace/two"},
	}); err == nil {
		t.Fatal("equivalent repository URLs accepted twice")
	}
	if err := ValidateGitRepositoryConflicts([]GitRepositorySpec{
		{URL: "https://git.internal/group/Repo", MountPath: "/workspace/one"},
		{URL: "https://git.internal/group/repo", MountPath: "/workspace/two"},
		{URL: "https://git.internal/group/repo.git", MountPath: "/workspace/four"},
	}); err != nil {
		t.Fatalf("distinct case-sensitive paths rejected: %v", err)
	}
}

func TestDefaultGitRepositoryMountPath(t *testing.T) {
	t.Parallel()

	if got := defaultGitRepositoryMountPath("https://git.internal/group/subgroup/widgets.git/"); got != "/workspace/widgets" {
		t.Fatalf("default repository mount path = %q", got)
	}
}
