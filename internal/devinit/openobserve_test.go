package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

func TestLocalOpenObserveRejectsMissingCredentialsAndPort(t *testing.T) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if err := configureLocalOpenObserve(root, t.TempDir(), 0); err == nil {
		t.Fatal("accepted missing port")
	}
	if err := configureLocalOpenObserve(root, t.TempDir(), 5080); err == nil {
		t.Fatal("accepted missing credentials")
	}
	repo := t.TempDir()
	path := filepath.Join(repo, ".run", "secrets", "openobserve.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"username":"admin@oma.local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := localOpenObserveCredentials(repo, true); err == nil {
		t.Fatal("replaced malformed credentials")
	}
}

func TestLocalOpenObservePersistsPrivateCredentialsAndGeneratesConfig(t *testing.T) {
	repo := t.TempDir()
	credentials, err := localOpenObserveCredentials(repo, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(credentials.Password, "Oma!") || len(credentials.Password) < 32 {
		t.Fatal("generated password does not satisfy OpenObserve policy")
	}
	again, err := localOpenObserveCredentials(repo, true)
	if err != nil || again != credentials {
		t.Fatal("credentials changed on restart")
	}
	info, err := os.Stat(filepath.Join(repo, ".run", "secrets", "openobserve.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("credentials are not private")
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if err := configureLocalOpenObserve(root, repo, 51234); err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := root.Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Observability.Enabled || cfg.Observability.OpenObserve.BaseURL != "http://127.0.0.1:51234" || cfg.Observability.OpenObserve.Organization != "default" || cfg.Observability.OpenObserve.LogsStream != "oma_dsh" {
		t.Fatal("local endpoint not configured")
	}
	if cfg.Observability.OpenObserve.Ingestion.Password != credentials.Password || cfg.Observability.OpenObserve.Query.Password != credentials.Password {
		t.Fatal("generated credentials not bound to local storage")
	}
}
