package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadImagesConfigRejectsInvalidInput(t *testing.T) {
	for _, data := range []string{
		"prefix: null\n", "unknown: example.test/team\n", "prefix: example.test/team\n---\nprefix: other.test/team\n",
		"prefix: https://example.test/team\n", "prefix: user:password@example.test/team\n", "prefix: example.test/team:tag\n",
		"prefix: ../team\n", "prefix: /\n", "prefix: 'example.test/$TOKEN'\n", "prefix: 'example.test/team image'\n",
	} {
		path := filepath.Join(t.TempDir(), "images.yaml")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadImagesConfigFile(path); err == nil {
			t.Fatalf("invalid image configuration accepted: %q", data)
		}
	}
}

func TestLoadImagesConfigRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.yaml")
	if err := os.WriteFile(target, []byte("prefix: example.test/team\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "images.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImagesConfigFile(path); err == nil {
		t.Fatal("symlink image configuration accepted")
	}
}

func TestLoadImagesConfigResolvesImageReferences(t *testing.T) {
	for _, scenario := range []struct{ data, prefix string }{
		{"", defaultImagePrefix},
		{"prefix: ''\n", defaultImagePrefix},
		{"prefix: ' registry.example.test:5000/team/ '\n", "registry.example.test:5000/team"},
	} {
		path := filepath.Join(t.TempDir(), "images.yaml")
		if scenario.data != "" {
			if err := os.WriteFile(path, []byte(scenario.data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		images, err := loadImagesConfigFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := images.reference("e2b-local"); got != scenario.prefix+"/e2b-local:latest" {
			t.Fatalf("gateway reference = %q", got)
		}
		for _, template := range []string{"", "managed-agent-sandbox", "oma/managed-agent-sandbox:latest", defaultImagePrefix + "/managed-agent-sandbox:latest"} {
			if got := images.sandboxTemplate(template); got != scenario.prefix+"/managed-agent-sandbox:latest" {
				t.Fatalf("sandbox reference = %q", got)
			}
		}
		if got := images.sandboxTemplate("custom/sandbox:dev"); got != "custom/sandbox:dev" {
			t.Fatal("explicit sandbox reference was changed")
		}
	}
}

func TestBaseConfigUsesConfiguredImagePrefix(t *testing.T) {
	root := sandboxConfigTestRepo(t)
	base, err := loadBaseConfigWithImages(root, imagesConfig{Prefix: "registry.example.test/team"})
	if err != nil {
		t.Fatal(err)
	}
	if base.E2B.Template != "registry.example.test/team/managed-agent-sandbox:latest" {
		t.Fatalf("template = %q", base.E2B.Template)
	}
	data, err := os.ReadFile(baseConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "registry.example.test") {
		t.Fatal("machine image prefix leaked into manual base config")
	}
}
