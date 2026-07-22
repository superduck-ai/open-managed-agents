package environments

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestNormalizePackages(t *testing.T) {
	t.Run("manager options are rejected for every manager without echoing the spec", func(t *testing.T) {
		for _, manager := range packageManagerNames {
			manager := manager
			t.Run(manager, func(t *testing.T) {
				secretOption := "  --token=secret-value"
				packages, err := normalizePackages(mustPackageJSON(t, map[string]any{
					"type":  managerPackageType,
					manager: []string{secretOption},
				}))
				if err == nil || packages != nil || !strings.Contains(err.Error(), invalidPackageOptionMessage) {
					t.Fatalf("normalizePackages() = (%#v, %v), want manager option error", packages, err)
				}
				if strings.Contains(err.Error(), secretOption) || strings.Contains(err.Error(), "secret-value") {
					t.Fatalf("normalization error leaked package option: %v", err)
				}
			})
		}
	})

	t.Run("invalid type is rejected", func(t *testing.T) {
		packages, err := normalizePackages(json.RawMessage(`{"type":"other","pip":["numpy"]}`))
		if err == nil || packages != nil || !strings.Contains(err.Error(), `type must be "packages"`) {
			t.Fatalf("normalizePackages() = (%#v, %v), want invalid type error", packages, err)
		}
	})
}

func TestBuildPackageManifest(t *testing.T) {
	t.Run("manager option in stored config is rejected without echoing the spec", func(t *testing.T) {
		secretOption := "-x=secret-value"
		manifest, provision, err := buildPackageManifest(mustPackageJSON(t, map[string]any{
			"type": "cloud",
			"packages": map[string]any{
				"type": managerPackageType,
				"pip":  []string{secretOption},
			},
		}))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), invalidPackageOptionMessage) {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want manager option error", manifest, provision, err)
		}
		if strings.Contains(err.Error(), secretOption) || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("manifest error leaked package option: %v", err)
		}
	})

	t.Run("invalid environment config has a root decode error", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{"type":`))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), "decode environment config") {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want root config decode error", manifest, provision, err)
		}
		if strings.Contains(err.Error(), "decode environment packages") {
			t.Fatalf("root config error used nested packages prefix: %v", err)
		}
	})

	t.Run("invalid packages fail the environment config schema", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{"type":"cloud","packages":true}`))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), "decode environment config") {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want environment config schema error", manifest, provision, err)
		}
	})

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

	t.Run("package array is rejected", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{
			"type":"cloud",
			"packages":[]
		}`))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), "decode environment config") {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want environment config schema error", manifest, provision, err)
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

func TestValidatePackageProvisioningResult(t *testing.T) {
	t.Run("prepare stage is restricted to apt", func(t *testing.T) {
		for _, testCase := range []struct {
			name     string
			exitCode int
			stdout   string
		}{
			{
				name:     "package manager",
				exitCode: 10,
				stdout:   `{"version":1,"status":"failed","category":"package_manager","manager":"npm","stage":"prepare","package_count":1,"duration_ms":1,"exit_code":17}`,
			},
			{
				name:     "timeout",
				exitCode: 11,
				stdout:   `{"version":1,"status":"failed","category":"timeout","manager":"npm","stage":"prepare","package_count":1,"duration_ms":1}`,
			},
			{
				name:     "internal",
				exitCode: 12,
				stdout:   `{"version":1,"status":"failed","category":"internal","manager":"npm","stage":"prepare","package_count":1,"duration_ms":1}`,
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				err := validatePackageProvisioningResult(e2bruntime.CommandResult{
					ExitCode: testCase.exitCode,
					Stdout:   []byte(testCase.stdout),
				})
				if err == nil || !strings.Contains(err.Error(), "inconsistent failure fields") {
					t.Fatalf("validatePackageProvisioningResult() = %v, want inconsistent failure fields", err)
				}
			})
		}
	})

	t.Run("protocol failures never echo stdout or stderr", func(t *testing.T) {
		secret := "secret-package-spec@example.test"
		for _, result := range []e2bruntime.CommandResult{
			{ExitCode: 0},
			{ExitCode: 0, Stdout: []byte(`{"version":1,"status":"succeeded","package_count":1,"duration_ms":1,"unknown":"` + secret + `"}`)},
			{ExitCode: 0, Stdout: []byte(`{"version":1,"status":"succeeded","package_count":1,"duration_ms":1} {}`)},
			{ExitCode: 10, Stdout: []byte(`{"version":1,"status":"failed","category":"future_category","stage":"install","duration_ms":1}`), Stderr: []byte(secret)},
		} {
			err := validatePackageProvisioningResult(result)
			if err == nil {
				t.Fatalf("validatePackageProvisioningResult(%q) succeeded", result.Stdout)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("protocol error leaked command output: %v", err)
			}
		}
	})

	t.Run("JSON and process exit code must agree", func(t *testing.T) {
		err := validatePackageProvisioningResult(e2bruntime.CommandResult{
			ExitCode: 10,
			Stdout:   []byte(`{"version":1,"status":"succeeded","package_count":1,"duration_ms":2}`),
		})
		if err == nil || !strings.Contains(err.Error(), "exit code") {
			t.Fatalf("validatePackageProvisioningResult() = %v, want exit-code mismatch", err)
		}
	})

	t.Run("package manager failure returns bounded structured diagnostics", func(t *testing.T) {
		err := validatePackageProvisioningResult(e2bruntime.CommandResult{
			ExitCode: 10,
			Stdout:   []byte(`{"version":1,"status":"failed","category":"package_manager","manager":"npm","stage":"install","package_count":6,"duration_ms":8,"exit_code":17}`),
		})
		if err == nil {
			t.Fatal("package manager failure was accepted")
		}
		for _, want := range []string{"category=package_manager", "manager=npm", "stage=install", "package_count=6", "duration_ms=8", "exit_code=17"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("failure diagnostic %q does not contain %q", err, want)
			}
		}
	})

	t.Run("success accepts the exact v1 result", func(t *testing.T) {
		err := validatePackageProvisioningResult(e2bruntime.CommandResult{
			ExitCode: 0,
			Stdout:   []byte("{\"version\":1,\"status\":\"succeeded\",\"package_count\":6,\"duration_ms\":12}\n"),
		})
		if err != nil {
			t.Fatalf("validatePackageProvisioningResult() error = %v", err)
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
