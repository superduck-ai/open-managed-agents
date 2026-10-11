package environments

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestRegistryProvisioningPreservesInputOutputAndCleansConfiguration(t *testing.T) {
	for _, exitCode := range []int{17, 0} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			dir := t.TempDir()
			manager := filepath.Join(dir, "manager 'with spaces")
			manifestPath := filepath.Join(dir, "manifest")
			configPath := filepath.Join(dir, "captured-npmrc")
			pathRecord := filepath.Join(dir, "temporary-path")
			registryRecord := filepath.Join(dir, "registry-environment")
			original := filepath.Join(dir, "original-npmrc")
			originalText := "//registry.example.com/:_authToken=fixture-token"
			if err := os.WriteFile(original, []byte(originalText), 0600); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nset -eu\ncat > " + shellQuote(manifestPath) + "\ncp \"$NPM_CONFIG_USERCONFIG\" " + shellQuote(configPath) + "\nprintf '%s' \"$NPM_CONFIG_USERCONFIG\" > " + shellQuote(pathRecord) + "\nprintf '%s' \"$NPM_CONFIG_REGISTRY\" > " + shellQuote(registryRecord) + "\nprintf '%s' '{\"version\":1,\"status\":\"succeeded\"}'\nexit " + strconv.Itoa(exitCode) + "\n"
			if err := os.WriteFile(manager, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{EnvironmentRunner: config.EnvironmentRunnerConfig{ManagerPath: manager}, EnvironmentPackages: config.EnvironmentPackageConfig{NPM: config.NPMRegistryConfig{Registry: "https://registry.npmjs.org/", ScopedRegistries: map[string]string{"@cvte": "https://registry.example.com/private;segment/"}}}}
			manifest := []byte(`{"version":1,"packages":{"npm":["@cvte/friday-cli"]}}`)
			command := exec.Command("sh", "-c", buildPackageProvisionCommand(cfg))
			command.Env = append(os.Environ(), "NPM_CONFIG_USERCONFIG="+original, "NPM_CONFIG_REGISTRY=https://previous.example.com/")
			command.Stdin = bytes.NewReader(manifest)
			output, err := command.Output()
			if command.ProcessState.ExitCode() != exitCode || (err == nil) != (exitCode == 0) {
				t.Fatalf("exit = %d, error = %v, want %d", command.ProcessState.ExitCode(), err, exitCode)
			}
			if string(output) != `{"version":1,"status":"succeeded"}` {
				t.Fatalf("wrapper changed manager stdout: %s", output)
			}
			if input := readPackageSourceFile(t, manifestPath); !bytes.Equal(input, manifest) {
				t.Fatalf("wrapper changed stdin: %s", input)
			}
			contents := string(readPackageSourceFile(t, configPath))
			if contents != originalText+"\nregistry=\"https://registry.npmjs.org/\"\n@cvte:registry=\"https://registry.example.com/private;segment/\"\n" {
				t.Fatalf("npm config did not retain authentication and append registry: %s", contents)
			}
			if string(readPackageSourceFile(t, original)) != originalText {
				t.Fatal("original npm configuration was modified")
			}
			if string(readPackageSourceFile(t, registryRecord)) != "https://registry.npmjs.org/" {
				t.Fatal("inherited registry overrode the deployment configuration")
			}
			if _, err := os.Stat(string(readPackageSourceFile(t, pathRecord))); !os.IsNotExist(err) {
				t.Fatalf("temporary configuration survived installation: %v", err)
			}
		})
	}
}

func TestRegistryProvisioningHandlesMissingHome(t *testing.T) {
	for _, test := range []struct {
		name          string
		home          string
		userConfigKey string
	}{
		{name: "HOME unset"},
		{name: "HOME empty", home: "empty"},
		{name: "uppercase userconfig without HOME", userConfigKey: "NPM_CONFIG_USERCONFIG"},
		{name: "lowercase userconfig without HOME", userConfigKey: "npm_config_userconfig"},
		{name: "HOME userconfig", home: "directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			manager := filepath.Join(dir, "manager")
			pathRecord := filepath.Join(dir, "temporary-path")
			original := filepath.Join(dir, ".npmrc")
			originalText := "//registry.example.com/:_authToken=fixture-token\n"
			if err := os.WriteFile(original, []byte(originalText), 0600); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nset -eu\ncat \"$NPM_CONFIG_USERCONFIG\"\nprintf '%s' \"$NPM_CONFIG_USERCONFIG\" > " + shellQuote(pathRecord) + "\n"
			if err := os.WriteFile(manager, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{EnvironmentRunner: config.EnvironmentRunnerConfig{ManagerPath: manager}, EnvironmentPackages: config.EnvironmentPackageConfig{NPM: config.NPMRegistryConfig{ScopedRegistries: map[string]string{"@cvte": "https://registry.example.com/"}}}}
			command := exec.Command("sh", "-c", buildPackageProvisionCommand(cfg))
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if key != "HOME" && key != "NPM_CONFIG_USERCONFIG" && key != "npm_config_userconfig" {
					command.Env = append(command.Env, entry)
				}
			}
			want := "\n@cvte:registry=\"https://registry.example.com/\"\n"
			if test.home == "empty" {
				command.Env = append(command.Env, "HOME=")
			} else if test.home == "directory" {
				command.Env = append(command.Env, "HOME="+dir)
				want = originalText + want
			}
			if test.userConfigKey != "" {
				command.Env = append(command.Env, test.userConfigKey+"="+original)
				want = originalText + want
			}
			output, err := command.CombinedOutput()
			if err != nil || string(output) != want {
				t.Fatalf("provisioning output = %q, error = %v, want %q", output, err, want)
			}
			if _, err := os.Stat(string(readPackageSourceFile(t, pathRecord))); !os.IsNotExist(err) {
				t.Fatalf("temporary configuration survived installation: %v", err)
			}
		})
	}
}

func readPackageSourceFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPrebuildScopedRegistryOptionsAndConfigurationIdentity(t *testing.T) {
	sources := config.EnvironmentPackageConfig{NPM: config.NPMRegistryConfig{ScopedRegistries: map[string]string{"@cvte": "https://registry.example.com/private/"}}}
	file := packageDockerfile("registry.example.com/base:stable", &environmentPackages{NPM: []string{"@cvte/friday-cli"}}, sources.NPM.Options())
	lines := strings.Split(strings.TrimSpace(file), "\n")
	var argv []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[len(lines)-1], "RUN ")), &argv); err != nil {
		t.Fatal(err)
	}
	want := []string{"npm", "install", "--global", "--@cvte:registry=https://registry.example.com/private/", "--", "@cvte/friday-cli"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("install command = %v, want %v", argv, want)
	}
	if strings.Contains(file, "npmrc") || strings.Contains(file, "ENV ") {
		t.Fatal("registry configuration was persisted in the image")
	}
	cfg := config.Config{}
	defaultKey := prebuildProviderKey(cfg)
	cfg.EnvironmentPackages = sources
	if prebuildProviderKey(cfg) == defaultKey {
		t.Fatal("registry change did not affect build configuration identity")
	}
	key := prebuildProviderKey(cfg)
	cfg.EnvironmentPackages.NPM.ScopedRegistries["@cvte"] = "https://other.example.com/"
	if prebuildProviderKey(cfg) == key {
		t.Fatal("different registry reused the same build configuration identity")
	}
}
