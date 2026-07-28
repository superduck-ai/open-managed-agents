package environments

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestBuildPackageProvisionCommand(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"default", "", "'/usr/local/bin/environment-manager' provision-packages --protocol v1 --stdin"},
		{"configured path is quoted", "/opt/env manager/bin/environment-manager", "'/opt/env manager/bin/environment-manager' provision-packages --protocol v1 --stdin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Config{}
			cfg.EnvironmentRunner.ManagerPath = test.path
			if got := buildPackageProvisionCommand(cfg); got != test.want {
				t.Fatalf("buildPackageProvisionCommand() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizePackages(t *testing.T) {
	t.Run("normalizes representative valid input", func(t *testing.T) {
		packages, err := normalizePackages(json.RawMessage(`{"type":"packages","apt":[" curl "],"npm":["@scope/pkg@1"],"pip":["requests[socks]"]}`))
		if err != nil {
			t.Fatal(err)
		}
		if packages.Type != managerPackageType || !reflect.DeepEqual(packages.APT, []string{" curl "}) ||
			!reflect.DeepEqual(packages.NPM, []string{"@scope/pkg@1"}) || !reflect.DeepEqual(packages.PIP, []string{"requests[socks]"}) {
			t.Fatalf("normalizePackages() = %#v", packages)
		}
	})

	invalid := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"invalid type", json.RawMessage(`{"type":"other","pip":["numpy"]}`), invalidPackagesTypeMessage},
		{"non-array", json.RawMessage(`{"type":"packages","pip":"numpy"}`), "config.packages.pip must be an array of strings"},
		{"blank", json.RawMessage(`{"type":"packages","apt":["  "]}`), "entries must be non-empty strings"},
		{"manager option", json.RawMessage(`{"type":"packages","pip":["--token=secret-value"]}`), invalidPackageOptionMessage},
		{"credential URL", json.RawMessage(`{"type":"packages","pip":["git+https://user:secret-token@example.test/private.git"]}`), "must not contain URL credentials"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			packages, err := normalizePackages(test.raw)
			if err == nil || packages != nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizePackages() = (%#v, %v), want %q", packages, err, test.want)
			}
			if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("normalization error leaked credentials: %v", err)
			}
		})
	}

	t.Run("API manifest above 1 MiB", func(t *testing.T) {
		packages, err := normalizePackages(mustPackageJSON(t, map[string]any{
			"type": managerPackageType,
			"pip":  []string{strings.Repeat("a", (1<<20)+1)},
		}))
		if err == nil || packages != nil || !strings.Contains(err.Error(), "1 MiB manifest limit") {
			t.Fatalf("normalizePackages() = (%#v, %v), want size error", packages, err)
		}
	})
}

func TestBuildPackageManifest(t *testing.T) {
	t.Run("malformed root", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{"type":`))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), "decode environment config") {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v)", manifest, provision, err)
		}
	})

	t.Run("stored dirty validation", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{"type":"cloud","packages":{"type":"packages","pip":["-x=secret-value"]}}`))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), invalidPackageOptionMessage) || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v)", manifest, provision, err)
		}
	})

	for _, test := range []struct {
		name string
		raw  string
	}{
		{"explicit empty", `{"type":"cloud","packages":{"type":"packages"}}`},
		{"absent packages", `{"type":"cloud"}`},
		{"null packages", `{"type":"cloud","packages":null}`},
		{"non-cloud", `{"type":"local","packages":{"type":"packages","pip":["numpy"]}}`},
	} {
		t.Run(test.name+" skips provisioning", func(t *testing.T) {
			manifest, provision, err := buildPackageManifest(json.RawMessage(test.raw))
			if err != nil || provision || manifest != nil {
				t.Fatalf("buildPackageManifest() = (%s, %t, %v)", manifest, provision, err)
			}
		})
	}

	t.Run("exact all-manager special-character JSON stdin", func(t *testing.T) {
		packages := environmentPackages{
			Type: managerPackageType,
			APT:  []string{"libfoo; echo nope"}, Cargo: []string{"crate@1 && false"},
			Gem: []string{"gem name 'quoted'"}, Go: []string{"example.test/mod@v1.2.3"},
			NPM: []string{"@scope/package@1.2.3"}, PIP: []string{`requests[socks] ; python_version >= "3.11"`},
		}
		manifest, provision, err := buildPackageManifest(mustPackageJSON(t, map[string]any{"type": "cloud", "packages": packages}))
		if err != nil || !provision {
			t.Fatalf("buildPackageManifest() provision = %t, error = %v", provision, err)
		}
		want := mustPackageJSON(t, packageManifest{Version: 1, Packages: packages})
		if !bytes.Equal(manifest, want) {
			t.Fatalf("manifest = %s, want exact JSON stdin %s", manifest, want)
		}
	})

	t.Run("stored manifest above 1 MiB", func(t *testing.T) {
		configJSON := mustPackageJSON(t, map[string]any{"type": "cloud", "packages": map[string]any{
			"type": managerPackageType, "pip": []string{strings.Repeat("a", (1<<20)+1)},
		}})
		manifest, provision, err := buildPackageManifest(configJSON)
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), "1 MiB manifest limit") {
			t.Fatalf("buildPackageManifest() = (%d bytes, %t, %v)", len(manifest), provision, err)
		}
	})
}

func TestValidatePackageProvisioningResult(t *testing.T) {
	t.Run("valid success", func(t *testing.T) {
		err := validatePackageProvisioningResult(e2bruntime.CommandResult{ExitCode: 0, Stdout: []byte(`{"version":1,"status":"succeeded","package_count":6,"duration_ms":12}`)})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("valid structured failure", func(t *testing.T) {
		err := validatePackageProvisioningResult(e2bruntime.CommandResult{ExitCode: 10, Stdout: []byte(`{"version":1,"status":"failed","category":"package_manager","manager":"npm","stage":"install","package_count":6,"duration_ms":8,"exit_code":17}`)})
		if err == nil {
			t.Fatal("failure was accepted")
		}
		for _, want := range []string{"category=package_manager", "manager=npm", "stage=install", "package_count=6", "duration_ms=8", "exit_code=17"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not contain %q", err, want)
			}
		}
	})

	rejected := []struct {
		name   string
		exit   int
		stdout string
		want   string
	}{
		{"failed with exit zero", 0, `{"version":1,"status":"failed","duration_ms":1}`, "process exit code 0"},
		{"negative duration", 0, `{"version":1,"status":"succeeded","package_count":1,"duration_ms":-1}`, "required field"},
		{"negative package count", 10, `{"version":1,"status":"failed","package_count":-1,"duration_ms":1}`, "negative package_count"},
		{"nonpositive manager exit code", 10, `{"version":1,"status":"failed","duration_ms":1,"exit_code":0}`, "manager exit_code"},
		{"success with failure field", 0, `{"version":1,"status":"succeeded","package_count":1,"duration_ms":1,"manager":"pip"}`, "inconsistent succeeded fields"},
		{"missing version", 0, `{"status":"succeeded","package_count":1,"duration_ms":1}`, "required field"},
		{"wrong version", 0, `{"version":2,"status":"succeeded","package_count":1,"duration_ms":1}`, "required field"},
		{"missing status", 0, `{"version":1,"package_count":1,"duration_ms":1}`, "required field"},
		{"wrong status type", 0, `{"version":1,"status":2,"package_count":1,"duration_ms":1}`, "decode failed"},
		{"missing duration", 0, `{"version":1,"status":"succeeded","package_count":1}`, "required field"},
		{"wrong duration type", 0, `{"version":1,"status":"succeeded","package_count":1,"duration_ms":"1"}`, "decode failed"},
		{"missing success count", 0, `{"version":1,"status":"succeeded","duration_ms":1}`, "inconsistent succeeded fields"},
		{"wrong success count type", 0, `{"version":1,"status":"succeeded","package_count":"1","duration_ms":1}`, "decode failed"},
		{"unknown status", 0, `{"version":1,"status":"pending","duration_ms":1}`, "unknown status"},
		{"unknown JSON field", 0, `{"version":1,"status":"succeeded","package_count":1,"duration_ms":1,"detail":"nope"}`, "decode failed"},
		{"multiple JSON values", 0, `{"version":1,"status":"succeeded","package_count":1,"duration_ms":1} {}`, "multiple JSON values"},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			err := validatePackageProvisioningResult(e2bruntime.CommandResult{ExitCode: test.exit, Stdout: []byte(test.stdout)})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}

	otherInvalid := []struct {
		name   string
		result e2bruntime.CommandResult
		want   string
	}{
		{"empty", e2bruntime.CommandResult{}, "decode failed"},
		{"malformed", e2bruntime.CommandResult{Stdout: []byte(`{"version":`)}, "decode failed"},
		{"exit status mismatch", e2bruntime.CommandResult{ExitCode: 10, Stdout: []byte(`{"version":1,"status":"succeeded","package_count":1,"duration_ms":2}`)}, "exit code"},
		{"oversized", e2bruntime.CommandResult{Stdout: bytes.Repeat([]byte("x"), maxPackageProvisioningResultBytes+1)}, "16 KiB limit"},
	}
	for _, test := range otherInvalid {
		t.Run(test.name, func(t *testing.T) {
			err := validatePackageProvisioningResult(test.result)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validatePackageProvisioningResult() = %v, want %q", err, test.want)
			}
		})
	}

	t.Run("unknown diagnostics are sanitized", func(t *testing.T) {
		const secret = "secret-package-output"
		err := validatePackageProvisioningResult(e2bruntime.CommandResult{ExitCode: 10, Stdout: []byte(`{"version":1,"status":"failed","category":"` + secret + `","manager":"` + secret + `","stage":"` + secret + `","duration_ms":8}`)})
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("sanitized diagnostics = %v", err)
		}
		for _, field := range []string{"category=unknown", "manager=unknown", "stage=unknown"} {
			if !strings.Contains(err.Error(), field) {
				t.Errorf("sanitized diagnostics %q missing %q", err, field)
			}
		}
	})
}

func mustPackageJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}
