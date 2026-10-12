package environments

import (
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

func deploymentSourceFixture() config.EnvironmentPackageConfig {
	return config.EnvironmentPackageConfig{
		NPM: config.NPMRegistryConfig{ScopedRegistries: map[string]string{"@example": "https://packages.example.internal/npm-private/"}},
		PIP: config.PIPRegistryConfig{IndexURL: "https://packages.example.internal/pypi/simple/"},
		Go:  config.GoRegistryConfig{Proxy: "https://packages.example.internal/goproxy/"},
		Cargo: config.CargoRegistryConfig{Registries: map[string]string{
			"example": "https://packages.example.internal/cargo/index/",
			"acme":    "https://acme.example.internal/cargo/index/",
		}},
		Gem: config.GemRegistryConfig{Sources: []string{"https://packages.example.internal/rubygems/", "https://acme.example.internal/gems/"}},
	}
}

func TestDeploymentSourcesProvisioningAndCleanup(t *testing.T) {
	for _, exitCode := range []int{19, 0} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			for _, manager := range []string{"cargo", "gem"} {
				text := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE/" + manager + "-args\"\n"
				if manager == "cargo" {
					text += "cp \"$2\" \"$CAPTURE/cargo-config\"\n"
				} else {
					text += "cp \"${GEMRC##*:}\" \"$CAPTURE/gem-config\"\nprintf '%s' \"$GEMRC\" > \"$CAPTURE/gemrc-paths\"\n"
					if _, err := exec.LookPath("ruby"); err == nil {
						text += "ruby -W0 -rrubygems -rjson -e 'print JSON.generate(Gem::ConfigFile.new([]).sources)' > \"$CAPTURE/native-gem-sources\"\n"
					}
				}
				writeSourceTestExecutable(t, filepath.Join(bin, manager), text)
			}
			manager := filepath.Join(bin, "environment-manager")
			writeSourceTestExecutable(t, manager, `#!/bin/sh
set -eu
cat > "$CAPTURE/manifest"
printf '%s\n' "$PIP_INDEX_URL" "$GOPROXY" "$GOSUMDB" "$NPM_CONFIG_REGISTRY" "$CARGO_HOME" > "$CAPTURE/env"
cp "$NPM_CONFIG_USERCONFIG" "$CAPTURE/npmrc"
printf '%s' "$OMA_PACKAGE_CONFIG_DIRECTORY" > "$CAPTURE/temporary-path"
cargo install 'crate@1.2.3'
gem install 'gem-name:1.2.3'
printf '%s' 'manager stdout'
printf '%s' 'manager stderr' >&2
exit `+strconv.Itoa(exitCode)+"\n")
			npmrc := filepath.Join(dir, "original-npmrc")
			original := "registry=https://image.example.internal/npm/\n//packages.example.internal/:_authToken=fixture-token\n"
			if err := os.WriteFile(npmrc, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			cargoHome := filepath.Join(dir, "cargo-home")
			if err := os.Mkdir(cargoHome, 0700); err != nil {
				t.Fatal(err)
			}
			cargoOriginal := "[registry]\ndefault = 'image-default'\n[registries.example]\ntoken = 'fixture-token'\n"
			cargoOriginalPath := filepath.Join(cargoHome, "config.toml")
			if err := os.WriteFile(cargoOriginalPath, []byte(cargoOriginal), 0600); err != nil {
				t.Fatal(err)
			}
			gemrc := filepath.Join(dir, "original-gemrc")
			gemOriginal := ":ssl_ca_cert: fixture-ca\n"
			if err := os.WriteFile(gemrc, []byte(gemOriginal), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{EnvironmentPackages: deploymentSourceFixture(), EnvironmentRunner: config.EnvironmentRunnerConfig{ManagerPath: manager}}
			command := exec.CommandContext(t.Context(), "sh", "-c", buildPackageProvisionCommand(cfg))
			command.Env = append(os.Environ(), "CAPTURE="+dir, "PATH="+bin+":"+os.Getenv("PATH"), "NPM_CONFIG_USERCONFIG="+npmrc,
				"NPM_CONFIG_REGISTRY=https://image.example.internal/npm/", "GOSUMDB=image-sumdb", "CARGO_HOME="+cargoHome, "GEMRC="+gemrc)
			manifest := `{"version":1,"packages":{"type":"packages","cargo":["crate@1.2.3"],"gem":["gem-name:1.2.3"]}}`
			command.Stdin = strings.NewReader(manifest)
			var stderr strings.Builder
			command.Stderr = &stderr
			stdout, err := command.Output()
			if (err == nil) != (exitCode == 0) || command.ProcessState.ExitCode() != exitCode {
				t.Fatalf("exit=%d error=%v stderr=%s", command.ProcessState.ExitCode(), err, stderr.String())
			}
			if string(stdout) != "manager stdout" || stderr.String() != "manager stderr" || string(readPackageSourceFile(t, filepath.Join(dir, "manifest"))) != manifest {
				t.Fatal("wrapper changed manager IO")
			}
			sources := cfg.EnvironmentPackages
			wantEnv := sources.PIP.IndexURL + "\n" + sources.Go.Proxy + "\nimage-sumdb\nhttps://image.example.internal/npm/\n" + cargoHome + "\n"
			if string(readPackageSourceFile(t, filepath.Join(dir, "env"))) != wantEnv {
				t.Fatal("registry environment did not preserve unspecified defaults")
			}
			wantCargo := "[registries.acme]\nindex = \"https://acme.example.internal/cargo/index/\"\n[registries.example]\nindex = \"https://packages.example.internal/cargo/index/\"\n"
			if string(readPackageSourceFile(t, filepath.Join(dir, "cargo-config"))) != wantCargo {
				t.Fatal("cargo overlay changed registry selection or omitted a source")
			}
			cargoArgs := strings.Split(strings.TrimSuffix(string(readPackageSourceFile(t, filepath.Join(dir, "cargo-args"))), "\n"), "\n")
			if len(cargoArgs) != 4 || cargoArgs[0] != "--config" || cargoArgs[2] != "install" || cargoArgs[3] != "crate@1.2.3" {
				t.Fatalf("cargo argv = %v", cargoArgs)
			}
			if string(readPackageSourceFile(t, filepath.Join(dir, "gem-args"))) != "install\ngem-name:1.2.3\n" || !strings.HasPrefix(string(readPackageSourceFile(t, filepath.Join(dir, "gemrc-paths"))), gemrc+":") {
				t.Fatal("gem did not retain its command and original configuration")
			}
			if string(readPackageSourceFile(t, filepath.Join(dir, "gem-config"))) != ":sources:\n- \""+sources.Gem.Sources[0]+"\"\n- \""+sources.Gem.Sources[1]+"\"\n" {
				t.Fatal("gem source configuration did not contain declared sources")
			}
			if _, err := exec.LookPath("ruby"); err == nil {
				var nativeSources []string
				if err := json.Unmarshal(readPackageSourceFile(t, filepath.Join(dir, "native-gem-sources")), &nativeSources); err != nil || !reflect.DeepEqual(nativeSources, sources.Gem.Sources) {
					t.Fatalf("native RubyGems sources=%v error=%v", nativeSources, err)
				}
			}
			if string(readPackageSourceFile(t, npmrc)) != original || string(readPackageSourceFile(t, cargoOriginalPath)) != cargoOriginal || string(readPackageSourceFile(t, gemrc)) != gemOriginal {
				t.Fatal("original authentication configuration was modified")
			}
			if string(readPackageSourceFile(t, filepath.Join(dir, "npmrc"))) != original+"\n@example:registry=\"https://packages.example.internal/npm-private/\"\n" {
				t.Fatal("scoped npm configuration did not preserve image defaults")
			}
			assertSourceConfigurationRemoved(t, dir)
		})
	}
}

func TestDeploymentSourcesCleanUpAfterSignal(t *testing.T) {
	dir := t.TempDir()
	manager := filepath.Join(dir, "manager")
	writeSourceTestExecutable(t, manager, "#!/bin/sh\nprintf '%s' \"$OMA_PACKAGE_CONFIG_DIRECTORY\" > \"$CAPTURE/temporary-path\"\nkill -TERM \"$PPID\"\n")
	cfg := config.Config{EnvironmentPackages: deploymentSourceFixture(), EnvironmentRunner: config.EnvironmentRunnerConfig{ManagerPath: manager}}
	command := exec.CommandContext(t.Context(), "sh", "-c", buildPackageProvisionCommand(cfg))
	command.Env = append(os.Environ(), "CAPTURE="+dir)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("terminated provisioning succeeded: %s", output)
	}
	assertSourceConfigurationRemoved(t, dir)
}

func TestDeploymentSourcesPrebuildRecipe(t *testing.T) {
	sources := deploymentSourceFixture()
	packages := &environmentPackages{NPM: []string{"@example/cli"}, PIP: []string{"demo >= 1"}, Go: []string{"example.internal/demo@v1"}, Cargo: []string{"crate@1"}, Gem: []string{"gem-name"}}
	file := packageDockerfile("registry.example.internal/base:stable", packages, sources)
	var commands [][]string
	for _, line := range strings.Split(file, "\n") {
		if encoded, ok := strings.CutPrefix(line, "RUN "); ok {
			var command []string
			if err := json.Unmarshal([]byte(encoded), &command); err != nil {
				t.Fatal(err)
			}
			commands = append(commands, command)
		}
	}
	want := [][]string{
		{"gem", "install", "--source", sources.Gem.Sources[0], "--source", sources.Gem.Sources[1], "gem-name"},
		{"env", "GOPROXY=" + sources.Go.Proxy, "go", "install", packages.Go[0]},
		{"npm", "install", "--global", "--@example:registry=" + sources.NPM.ScopedRegistries["@example"], "--", packages.NPM[0]},
		{"env", "PIP_INDEX_URL=" + sources.PIP.IndexURL, "pip", "install", packages.PIP[0]},
	}
	if len(commands) != 5 || !reflect.DeepEqual(commands[1:], want) {
		t.Fatalf("prebuild commands = %v", commands)
	}
	cargo := commands[0]
	if len(cargo) != 7 || cargo[0] != "sh" || cargo[1] != "-c" || !strings.Contains(cargo[2], "trap ") || !reflect.DeepEqual(cargo[4:], []string{"cargo", "install", "crate@1"}) {
		t.Fatalf("cargo command = %v", cargo)
	}
	if strings.Contains(file, "\nENV ") || strings.Contains(file, "npmrc") {
		t.Fatal("prebuild persisted npm configuration or source environment")
	}
}

func TestDeploymentSourcesEachManagerChangesProviderKey(t *testing.T) {
	sources := deploymentSourceFixture()
	for _, source := range []config.EnvironmentPackageConfig{
		{NPM: sources.NPM}, {PIP: sources.PIP}, {Go: sources.Go}, {Cargo: sources.Cargo}, {Gem: sources.Gem},
	} {
		originalKey := prebuildProviderKey(config.Config{})
		cfg := config.Config{EnvironmentPackages: source}
		if prebuildProviderKey(cfg) == originalKey {
			t.Fatalf("source did not change provider key: %#v", source)
		}
		changed := config.Config{}
		data, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(strings.ReplaceAll(string(data), "packages.example.internal", "changed.example.internal")), &changed.EnvironmentPackages); err != nil {
			t.Fatal(err)
		}
		if prebuildProviderKey(changed) == prebuildProviderKey(cfg) {
			t.Fatalf("changed source reused provider key: %#v", source)
		}
	}
}

func writeSourceTestExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
		t.Fatal(err)
	}
}

func assertSourceConfigurationRemoved(t *testing.T, dir string) {
	t.Helper()
	temporary := string(readPackageSourceFile(t, filepath.Join(dir, "temporary-path")))
	if temporary == "" {
		t.Fatal("missing temporary configuration path")
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary configuration survived provisioning: %v", err)
	}
}
