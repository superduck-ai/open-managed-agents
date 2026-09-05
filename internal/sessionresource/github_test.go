package sessionresource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestNormalizeGitHubSpecRejectsUnsafeInputs(t *testing.T) {
	cases := []struct{ name, url, mount, checkout string }{
		{"url credentials", `"https://token@github.com/owner/repo"`, "", ""},
		{"http", `"http://github.com/owner/repo"`, "", ""},
		{"host spoof", `"https://github.com.evil.test/owner/repo"`, "", ""},
		{"port", `"https://github.com:443/owner/repo"`, "", ""},
		{"suffix", `"https://github.com/owner/repo.git"`, "", ""},
		{"query", `"https://github.com/owner/repo?x=1"`, "", ""},
		{"fragment", `"https://github.com/owner/repo#"`, "", ""},
		{"escaped path", `"https://github.com/owner/%72epo"`, "", ""},
		{"trailing slash", `"https://github.com/owner/repo/"`, "", ""},
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
			if _, err := NormalizeGitHubSpec(json.RawMessage(test.url), json.RawMessage(test.mount), json.RawMessage(test.checkout)); err == nil {
				t.Fatal("invalid resource accepted")
			}
		})
	}
}

func TestGitHubSpecsRejectAmbiguousRepositoriesAndPaths(t *testing.T) {
	for _, specs := range [][]GitHubSpec{
		{{URL: "https://github.com/Owner/Repo", MountPath: "/workspace/first"}, {URL: "https://github.com/owner/repo", MountPath: "/workspace/second"}},
		{{URL: "https://github.com/a/one", MountPath: "/workspace/repo"}, {URL: "https://github.com/a/two", MountPath: "/workspace/repo/child"}},
		{{URL: "https://github.com/a/one", MountPath: "/workspace/repo"}, {URL: "https://github.com/a/two", MountPath: "/workspace/repo"}},
	} {
		if err := ValidateGitHubSpecs(specs); err == nil {
			t.Fatal("ambiguous resource set accepted")
		}
	}
}

func TestNormalizeGitHubSpecDefaultsAndCheckout(t *testing.T) {
	for _, checkout := range []string{"", "null", `{"type":"branch","name":"feature/git-resources"}`, `{"type":"commit","sha":"` + strings.Repeat("A", 40) + `"}`} {
		spec, err := NormalizeGitHubSpec(json.RawMessage(`"https://github.com/owner/repo"`), nil, json.RawMessage(checkout))
		if err != nil || spec.MountPath != "/workspace/repo" {
			t.Fatalf("spec = %+v, err = %v", spec, err)
		}
		payload, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseStoredGitHubSpec(payload); err != nil {
			t.Fatal(err)
		}
	}
	spec, err := NormalizeGitHubSpec(json.RawMessage(`"https://github.com/owner/.github"`), nil, nil)
	if err != nil || spec.MountPath != "/workspace/.github" {
		t.Fatalf("dot-prefixed repository default = %+v, %v", spec, err)
	}
	if !validGitBranch(strings.Repeat("汉", 255)) || validGitBranch(strings.Repeat("汉", 256)) {
		t.Fatal("branch length must count characters")
	}
	if _, err := ParseStoredGitHubSpec(json.RawMessage(`{"url":"https://github.com/owner/repo"}`)); err == nil {
		t.Fatal("stored missing path received a default")
	}
}

func TestGitHubTokenEnvelopeRejectsPlaintextAndDifferentOwners(t *testing.T) {
	ctx := context.Background()
	service, err := secrets.NewLocalService(ctx, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	binding := secrets.ResourceBinding{OrganizationUUID: "org", WorkspaceUUID: "ws", OwnerKind: "session", OwnerID: "ses_one", ResourceID: "sesrsc_one"}
	if _, err := OpenGitHubToken(ctx, service, binding, json.RawMessage(`{"authorization_token":"legacy"}`)); err == nil {
		t.Fatal("plaintext token accepted")
	}
	raw, err := SealGitHubToken(ctx, service, binding, "github-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "github-test-secret") || strings.Contains(string(raw), "authorization_token") {
		t.Fatal("plaintext token persisted")
	}
	wrong := binding
	wrong.OwnerID = "ses_other"
	if _, err := OpenGitHubToken(ctx, service, wrong, raw); err == nil {
		t.Fatal("cross-session token accepted")
	}
	token, err := OpenGitHubToken(ctx, service, binding, raw)
	if err != nil || token != "github-test-secret" {
		t.Fatalf("token decrypt failed: %v", err)
	}
	for _, raw := range []string{`123`, `{}`, `"token\r\ninjected"`, `" token "`} {
		if _, err := ParseGitHubToken(json.RawMessage(raw)); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}

func TestAnonymousGitHubToken(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`""`)} {
		token, err := ParseGitHubToken(raw)
		if err != nil || token != "" {
			t.Fatalf("parse anonymous: %q %v", token, err)
		}
		stored, err := SealGitHubToken(context.Background(), nil, secrets.ResourceBinding{}, token)
		if err != nil || string(stored) != "null" {
			t.Fatalf("seal anonymous: %s %v", stored, err)
		}
		token, err = OpenGitHubToken(context.Background(), nil, secrets.ResourceBinding{}, stored)
		if err != nil || token != "" {
			t.Fatalf("open anonymous: %q %v", token, err)
		}
	}
}

func TestAnonymousGitHubTokenFromSQLNull(t *testing.T) {
	token, err := OpenGitHubToken(context.Background(), nil, secrets.ResourceBinding{}, nil)
	if err != nil || token != "" {
		t.Fatalf("SQL NULL credential: %q %v", token, err)
	}
}
