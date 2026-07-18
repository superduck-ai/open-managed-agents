package environments

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizePackages(t *testing.T) {
	t.Run("invalid type is rejected", func(t *testing.T) {
		packages, err := normalizePackages(json.RawMessage(`{"type":"other","pip":["numpy"]}`))
		if err == nil || packages != nil || !strings.Contains(err.Error(), `type must be "packages"`) {
			t.Fatalf("normalizePackages() = (%#v, %v), want invalid type error", packages, err)
		}
	})
}

func TestBuildPackageManifest(t *testing.T) {
	t.Run("invalid type uses the normalization error", func(t *testing.T) {
		_, normalizeErr := normalizePackages(json.RawMessage(`{"type":"other"}`))
		manifest, provision, manifestErr := buildPackageManifest(json.RawMessage(`{
			"type":"cloud",
			"packages":{"type":"other"}
		}`))
		if normalizeErr == nil || manifestErr == nil || provision || manifest != nil {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), normalize error = %v", manifest, provision, manifestErr, normalizeErr)
		}
		if manifestErr.Error() != normalizeErr.Error() {
			t.Fatalf("manifest error = %q, want normalization error %q", manifestErr, normalizeErr)
		}
	})

	t.Run("credential-bearing URL is rejected without echoing the spec", func(t *testing.T) {
		secretSpec := "git+https://user:secret-token@example.test/private.git"
		manifest, provision, err := buildPackageManifest(mustPackageJSON(t, map[string]any{
			"type": "cloud",
			"packages": map[string]any{
				"type": "packages",
				"pip":  []string{secretSpec},
			},
		}))
		if err == nil || provision || manifest != nil {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want rejected manifest", manifest, provision, err)
		}
		if strings.Contains(err.Error(), secretSpec) || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("manifest error leaked package credentials: %v", err)
		}
	})

	t.Run("legacy empty package array skips provisioning", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{
			"type":"cloud",
			"packages":[]
		}`))
		if err != nil || provision || manifest != nil {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want (nil, false, nil)", manifest, provision, err)
		}
	})

	t.Run("empty packages skip provisioning", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{
			"type":"cloud",
			"packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]}
		}`))
		if err != nil || provision || manifest != nil {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want (nil, false, nil)", manifest, provision, err)
		}
	})

	t.Run("special characters remain JSON data", func(t *testing.T) {
		specs := []string{
			"@scope/package@1.2.3",
			`requests[socks] @ https://example.test/archive.whl ; python_version >= "3.11"`,
			"package name; touch /tmp/oma-package-spec-was-shell",
		}
		config := mustPackageJSON(t, map[string]any{
			"type": "cloud",
			"packages": map[string]any{
				"type": "packages",
				"npm":  []string{specs[0]},
				"pip":  specs[1:],
			},
		})
		manifest, provision, err := buildPackageManifest(config)
		if err != nil || !provision {
			t.Fatalf("buildPackageManifest() provision = %t, error = %v", provision, err)
		}
		var decoded packageManifest
		if err := json.Unmarshal(manifest, &decoded); err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		if decoded.Version != 1 || !reflect.DeepEqual(decoded.Packages.NPM, specs[:1]) || !reflect.DeepEqual(decoded.Packages.PIP, specs[1:]) {
			t.Fatalf("manifest changed package specs: %#v", decoded)
		}
		if strings.Contains(packageProvisionCommand, specs[0]) || strings.Contains(packageProvisionCommand, specs[2]) {
			t.Fatalf("fixed provision command contains a package spec: %q", packageProvisionCommand)
		}
		if bytes.Contains(manifest, []byte(":null")) {
			t.Fatalf("manifest contains null package manager arrays: %s", manifest)
		}
	})
}

func TestPackageProvisionerUsesManagerOrderAndStopsOnFailure(t *testing.T) {
	python := "/usr/bin/python3"
	if _, statErr := os.Stat(python); statErr != nil {
		python = "python3"
	}
	python, err := exec.LookPath(python)
	if err != nil {
		t.Skip("python3 is required to exercise the embedded provisioner")
	}
	tests := []struct {
		name       string
		fail       string
		secretSpec string
		want       []string
	}{
		{
			name:       "first failure stops later managers without logging specs",
			fail:       "gem",
			secretSpec: "https://user:secret-token@example.test/private.gem",
			want: []string{
				"apt-get|update", "apt-get|install|-y|--|ffmpeg",
				"cargo|install|ripgrep@14.1.1", "gem|install|https://user:secret-token@example.test/private.gem",
			},
		},
		{
			name: "all managers in contract order and Go specs are separate",
			want: []string{
				"apt-get|update", "apt-get|install|-y|--|ffmpeg",
				"cargo|install|ripgrep@14.1.1", "gem|install|rake:13.2.1",
				"go|install|golang.org/x/tools/cmd/goimports@v0.35.0",
				"go|install|github.com/google/addlicense@v1.1.1",
				"npm|install|--global|--|@scope/package@5.9.3",
				`pip|install|requests[socks] @ https://example.test/a.whl ; python_version >= "3.11"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "commands.log")
			fake := []byte("#!/bin/sh\nbase=${0##*/}\nprintf '%s' \"$base\" >> \"$OMA_PACKAGE_TEST_LOG\"\nfor arg in \"$@\"; do printf '|%s' \"$arg\" >> \"$OMA_PACKAGE_TEST_LOG\"; done\nprintf '\\n' >> \"$OMA_PACKAGE_TEST_LOG\"\n[ \"$base\" = \"$OMA_PACKAGE_TEST_FAIL\" ] && exit 17\nexit 0\n")
			for _, name := range []string{"apt-get", "cargo", "gem", "go", "npm", "pip"} {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, fake, 0o755); err != nil {
					t.Fatalf("write fake %s: %v", name, err)
				}
			}
			manifestPath := filepath.Join(dir, "packages.json")
			gemSpec := "rake:13.2.1"
			if tt.secretSpec != "" {
				gemSpec = tt.secretSpec
			}
			manifest := packageManifest{Version: 1, Packages: environmentPackages{
				Type: "packages", APT: []string{"ffmpeg"}, Cargo: []string{"ripgrep@14.1.1"},
				Gem: []string{gemSpec}, Go: []string{"golang.org/x/tools/cmd/goimports@v0.35.0", "github.com/google/addlicense@v1.1.1"},
				NPM: []string{"@scope/package@5.9.3"},
				PIP: []string{`requests[socks] @ https://example.test/a.whl ; python_version >= "3.11"`},
			}}
			if err := os.WriteFile(manifestPath, mustPackageJSON(t, manifest), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			command := exec.Command(python, "-", manifestPath)
			command.Stdin = bytes.NewReader(packageProvisionerV1)
			command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "OMA_PACKAGE_TEST_LOG="+logPath, "OMA_PACKAGE_TEST_FAIL="+tt.fail)
			output, err := command.CombinedOutput()
			if tt.fail == "" && err != nil {
				t.Fatalf("run provisioner: %v: %s", err, output)
			}
			if tt.fail != "" && err == nil {
				t.Fatal("provisioner succeeded, want manager failure")
			}
			if tt.secretSpec != "" && strings.Contains(string(output), tt.secretSpec) {
				t.Fatalf("provisioner output leaked package spec: %s", output)
			}
			log, readErr := os.ReadFile(logPath)
			if readErr != nil {
				t.Fatalf("read command log: %v", readErr)
			}
			got := strings.Split(strings.TrimSpace(string(log)), "\n")
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("manager calls = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func mustPackageJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}
