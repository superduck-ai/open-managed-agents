package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderE2BConfigTemplate(t *testing.T) {
	template := `
server:
  addr: "0.0.0.0:3099"
docker:
  container_name_prefix: "e2b-envd-"
  envd_binary: "__ENVD_BIN_DIR__/envd-linux-amd64"
`
	rendered := renderE2BConfigTemplate(template, "feature-x", 43123, "/work/envd-bin", "arm64")
	for _, want := range []string{
		`addr: "0.0.0.0:43123"`,
		`container_name_prefix: "e2b-envd-feature-x-"`,
		`envd_binary: "/work/envd-bin/envd-linux-arm64"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered config missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "__ENVD_BIN_DIR__") || strings.Contains(rendered, ":3099") {
		t.Errorf("unrendered placeholder left behind:\n%s", rendered)
	}
}

func sandboxConfigTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := baseConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(minimalBaseConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRefreshHealthyStackRetriesFailedImageChange(t *testing.T) {
	root := sandboxConfigTestRepo(t)
	deploy := filepath.Join(root, "deploy", "docker-compose")
	if err := os.MkdirAll(deploy, 0o700); err != nil {
		t.Fatal(err)
	}
	template := "docker:\n  envd_binary: __ENVD_BIN_DIR__/envd-linux-amd64\n"
	if err := os.WriteFile(filepath.Join(deploy, "e2b-local.yaml"), []byte(template), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(generatedDir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(generatedDir(root), "e2b-local.yaml")
	if err := os.WriteFile(path, []byte("old configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\nimage) echo arm64;;\n*) echo refresh-failed >&2; exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ports := StackPorts{E2BPort: 43123}
	binding := EnvBinding{Name: "test"}
	if err := writeStackFiles(t.Context(), root, binding, ports); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(generatedDir(root), "stack.env")
	previousEnv, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(previousEnv), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "OMA_E2B_LOCAL_IMAGE=") {
			lines[index] = "OMA_E2B_LOCAL_IMAGE=legacy.example.test/e2b-local:latest"
		}
	}
	previousEnv = []byte(strings.Join(lines, "\n"))
	if err := os.WriteFile(envPath, previousEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, err := refreshHealthyStack(t.Context(), root, binding, ports)
		if err == nil || !strings.Contains(err.Error(), "refresh-failed") {
			t.Fatalf("refresh failure was lost: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(previous) {
			t.Fatalf("failed refresh lost previous configuration: %q, %v", data, err)
		}
		data, err = os.ReadFile(envPath)
		if err != nil || string(data) != string(previousEnv) {
			t.Fatal("failed image refresh lost previous stack environment")
		}
	}
}
