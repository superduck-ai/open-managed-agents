package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func testBaseConfig() config.Config {
	return config.Config{
		Server:   config.ServerConfig{Addr: "127.0.0.1:38080"},
		Database: config.DatabaseConfig{URL: "postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable"},
		Redis:    config.RedisConfig{URL: "redis://localhost:6379"},
		Storage: config.StorageConfig{S3: config.S3Config{
			Bucket: "claude-files",
		}},
	}
}

func testBinding() EnvBinding {
	return EnvBinding{
		Name:     "feature-x",
		EnvID:    "7d5adf3a-9d08-4179-9cc4-8721d3e83963",
		Worktree: "/repo",
		InitFile: "/repo/dev-init.yaml",
	}
}

func TestNewLayoutDerivesRoutes(t *testing.T) {
	layout, err := NewLayout(testBinding(), testBaseConfig(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if layout.APIRoute != "oma-feature-x-api" || layout.WebRoute != "oma-feature-x-web" {
		t.Errorf("routes = %q / %q", layout.APIRoute, layout.WebRoute)
	}
	if layout.WebPublicURL != "http://oma-feature-x-web.localhost" {
		t.Errorf("WebPublicURL = %q", layout.WebPublicURL)
	}
	for name, path := range map[string]string{
		"config.yaml": layout.GeneratedConfig, "state.json": layout.StateFile, "credentials.json": layout.CredentialsFile,
	} {
		if want := filepath.Join("/repo", ".run", "generated", name); path != want {
			t.Errorf("%s = %q, want %q", name, path, want)
		}
	}
	derived, _, err := deriveDatabaseURL(testBaseConfig().Database.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(derived, "/claude_api?") {
		t.Errorf("deriveDatabaseURL() = %q", derived)
	}
	withPort, err := replaceURLPort(derived, 45480)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withPort, "localhost:45480/claude_api?") {
		t.Errorf("replaceURLPort() = %q", withPort)
	}
	redisURL, err := redisURLWithPort(testBaseConfig().Redis.URL, 62123)
	if err != nil {
		t.Fatal(err)
	}
	if redisURL != "redis://localhost:62123" {
		t.Errorf("redisURLWithPort() = %q", redisURL)
	}
}

func TestNewLayoutRejectsInvalidInput(t *testing.T) {
	cases := map[string]EnvBinding{
		"empty name":   {Name: "", EnvID: "e"},
		"empty env id": {Name: "x", EnvID: ""},
	}
	for name, binding := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewLayout(binding, testBaseConfig(), "/repo"); err == nil {
				t.Fatalf("NewLayout() error = nil for %s, want failure", name)
			}
		})
	}
}

func TestProbeFreePort(t *testing.T) {
	port, err := ProbeFreePort(47000)
	if err != nil {
		t.Fatal(err)
	}
	if port < 47000 {
		t.Fatalf("ProbeFreePort() = %d, want >= 47000", port)
	}
	second, err := ProbeFreePort(port)
	if err != nil {
		t.Fatal(err)
	}
	if second < port {
		t.Fatalf("ProbeFreePort(%d) = %d", port, second)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"open-managed-agents": "open-managed-agents",
		"6abc":                "6abc",
		"My_Feature Branch.X": "my-feature-branch-x",
		"---":                 "env",
	}
	for raw, want := range cases {
		if got := slugify(raw); got != want {
			t.Errorf("slugify(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWorktreeSlug(t *testing.T) {
	main := t.TempDir()
	if err := os.MkdirAll(filepath.Join(main, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := worktreeSlug(main); got != slugify(filepath.Base(main)) {
		t.Errorf("main checkout slug = %q", got)
	}
	parent := t.TempDir()
	linked := filepath.Join(parent, "open-managed-agents")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := worktreeSlug(linked); got != slugify(filepath.Base(parent)) {
		t.Errorf("linked worktree slug = %q, want parent name", got)
	}
}
