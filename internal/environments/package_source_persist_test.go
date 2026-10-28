package environments

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestPackageSourcePersistScriptEmptyWithoutSources(t *testing.T) {
	if script := packageSourcePersistScript(config.EnvironmentPackageConfig{}); script != "" {
		t.Fatalf("script = %q", script)
	}
}

func TestPackageSourcePersistScriptRewritesManagerConfigIdempotently(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required to rewrite pip.conf")
	}
	dir := t.TempDir()
	npmrc := filepath.Join(dir, "npmrc")
	pipConf := filepath.Join(dir, "pip.conf")
	cargoHome := filepath.Join(dir, "cargo")
	writeTestFile(t, npmrc, "registry=https://registry.npmmirror.com\ncache=/tmp/npm\n")
	writeTestFile(t, pipConf, "[global]\nindex-url = https://mirrors.aliyun.com/pypi/simple/\n\n[install]\nno-compile = true\n")
	script := packageSourcePersistScript(config.EnvironmentPackageConfig{
		NPM: config.NPMRegistryConfig{
			Registry:         "https://packages.example.internal/npm/",
			ScopedRegistries: map[string]string{"@example": "https://packages.example.internal/npm-private/"},
		},
		PIP:   config.PIPRegistryConfig{IndexURL: "https://packages.example.internal/pypi/simple/"},
		Cargo: config.CargoRegistryConfig{Registries: map[string]string{"example": "https://packages.example.internal/cargo/index/"}},
	})
	for range 2 {
		command := exec.Command("sh", "-c", script)
		command.Env = append(os.Environ(), "HOME="+dir, "NPM_CONFIG_USERCONFIG="+npmrc, "PIP_CONFIG_FILE="+pipConf, "CARGO_HOME="+cargoHome)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("persist script failed: %v\n%s", err, output)
		}
	}

	npm := readTestFile(t, npmrc)
	if strings.Count(npm, "# >>> oma package sources >>>") != 1 || !strings.Contains(npm, "cache=/tmp/npm") ||
		!strings.HasSuffix(npm, "registry=\"https://packages.example.internal/npm/\"\n@example:registry=\"https://packages.example.internal/npm-private/\"\n# <<< oma package sources <<<\n") {
		t.Fatalf("npmrc =\n%s", npm)
	}
	pip := readTestFile(t, pipConf)
	if !strings.Contains(pip, "index-url = https://packages.example.internal/pypi/simple/") || strings.Contains(pip, "aliyun") || !strings.Contains(pip, "no-compile = true") {
		t.Fatalf("pip.conf =\n%s", pip)
	}
	cargo := readTestFile(t, filepath.Join(cargoHome, "config.toml"))
	if strings.Count(cargo, "[registries.example]") != 1 {
		t.Fatalf("cargo config =\n%s", cargo)
	}
	if _, err := exec.LookPath("npm"); err == nil {
		for key, want := range map[string]string{"registry": "https://packages.example.internal/npm/", "@example:registry": "https://packages.example.internal/npm-private/"} {
			command := exec.Command("npm", "config", "get", key)
			command.Env = append(os.Environ(), "NPM_CONFIG_USERCONFIG="+npmrc, "npm_config_userconfig="+npmrc)
			output, err := command.Output()
			if err != nil || strings.TrimSpace(string(output)) != want {
				t.Fatalf("npm config get %s = %q, %v", key, output, err)
			}
		}
	}
}

func TestTemplateDockerfilePersistsSourcesBeforeInitScript(t *testing.T) {
	spec := &environmentBuildSpec{Packages: &environmentPackages{NPM: []string{"left-pad"}}, InitScript: "make"}
	file := templateDockerfile("registry.example.com/base:1", spec, config.EnvironmentPackageConfig{PIP: config.PIPRegistryConfig{IndexURL: "https://packages.example.internal/pypi/simple/"}})
	npm := strings.Index(file, `RUN ["npm"`)
	persist := strings.Index(file, `RUN ["sh","-c","set -eu`)
	script := strings.Index(file, `RUN ["bash","-eo","pipefail","-c","make"]`)
	if npm < 0 || persist < npm || script < persist {
		t.Fatalf("unexpected Dockerfile:\n%s", file)
	}
	if strings.Contains(templateDockerfile("registry.example.com/base:1", spec, config.EnvironmentPackageConfig{}), `RUN ["sh","-c"`) {
		t.Fatal("persist step added without configured sources")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
