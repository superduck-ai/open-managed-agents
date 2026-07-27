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
	t.Run("default manager path", func(t *testing.T) {
		if got := buildPackageProvisionCommand(config.Config{}); got != "'/usr/local/bin/environment-manager' provision-packages --protocol v1 --stdin" {
			t.Fatalf("buildPackageProvisionCommand() = %q, want the default quoted path", got)
		}
	})

	t.Run("configured manager path is honored and shell quoted", func(t *testing.T) {
		cfg := config.Config{}
		cfg.EnvironmentRunner.ManagerPath = "/opt/env manager/bin/environment-manager"
		if got := buildPackageProvisionCommand(cfg); got != "'/opt/env manager/bin/environment-manager' provision-packages --protocol v1 --stdin" {
			t.Fatalf("buildPackageProvisionCommand() = %q, want the configured quoted path", got)
		}
	})
}

func TestNormalizePackages(t *testing.T) {
	t.Run("manager options are rejected without echoing the spec", func(t *testing.T) {
		secretOption := "  --token=secret-value"
		packages, err := normalizePackages(mustPackageJSON(t, map[string]any{
			"type": managerPackageType,
			"pip":  []string{secretOption},
		}))
		if err == nil || packages != nil || !strings.Contains(err.Error(), invalidPackageOptionMessage) {
			t.Fatalf("normalizePackages() = (%#v, %v), want manager option error", packages, err)
		}
		if strings.Contains(err.Error(), secretOption) || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("normalization error leaked package option: %v", err)
		}
	})

	t.Run("credential-bearing URL is rejected without echoing the spec", func(t *testing.T) {
		secretSpec := "git+https://user:secret-token@example.test/private.git"
		packages, err := normalizePackages(mustPackageJSON(t, map[string]any{
			"type": managerPackageType,
			"pip":  []string{secretSpec},
		}))
		if err == nil || packages != nil {
			t.Fatalf("normalizePackages() = (%#v, %v), want credential URL error", packages, err)
		}
		if strings.Contains(err.Error(), secretSpec) || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("normalization error leaked package credentials: %v", err)
		}
	})

	t.Run("invalid type is rejected", func(t *testing.T) {
		packages, err := normalizePackages(json.RawMessage(`{"type":"other","pip":["numpy"]}`))
		if err == nil || packages != nil || !strings.Contains(err.Error(), `type must be "packages"`) {
			t.Fatalf("normalizePackages() = (%#v, %v), want invalid type error", packages, err)
		}
	})

	t.Run("non-array manager value names the offending manager", func(t *testing.T) {
		packages, err := normalizePackages(json.RawMessage(`{"type":"packages","pip":"numpy"}`))
		if err == nil || packages != nil || err.Error() != "config.packages.pip must be an array of strings" {
			t.Fatalf("normalizePackages() = (%#v, %v), want pip array error", packages, err)
		}
	})

	t.Run("blank and overlong specs are rejected", func(t *testing.T) {
		blank, blankErr := normalizePackages(json.RawMessage(`{"type":"packages","apt":["  "]}`))
		if blankErr == nil || blank != nil || !strings.Contains(blankErr.Error(), "config.packages.apt entries must be non-empty strings") {
			t.Fatalf("normalizePackages() = (%#v, %v), want blank spec error", blank, blankErr)
		}
		overlong, overlongErr := normalizePackages(mustPackageJSON(t, map[string]any{
			"type": managerPackageType,
			"npm":  []string{strings.Repeat("a", maxPackageSpecLength+1)},
		}))
		if overlongErr == nil || overlong != nil || !strings.Contains(overlongErr.Error(), "config.packages.npm entries must be at most 255 characters") {
			t.Fatalf("normalizePackages() = (%#v, %v), want overlong spec error", overlong, overlongErr)
		}
	})

	t.Run("manifest above the 1 MiB limit is rejected", func(t *testing.T) {
		oversized := make([]string, 0, 5000)
		for len(oversized) < 5000 {
			oversized = append(oversized, strings.Repeat("a", maxPackageSpecLength))
		}
		packages, err := normalizePackages(mustPackageJSON(t, map[string]any{
			"type": managerPackageType,
			"pip":  oversized,
		}))
		if err == nil || packages != nil || !strings.Contains(err.Error(), "1 MiB manifest limit") {
			t.Fatalf("normalizePackages() = (%#v, %v), want 1 MiB manifest limit error", packages, err)
		}
	})

	t.Run("absent packages normalize to empty lists", func(t *testing.T) {
		packages, err := normalizePackages(nil)
		if err != nil || packages == nil {
			t.Fatalf("normalizePackages(nil) = (%#v, %v)", packages, err)
		}
		if packages.Type != managerPackageType || !packages.empty() {
			t.Fatalf("normalizePackages(nil) = %#v, want typed empty packages", packages)
		}
		encoded := mustPackageJSON(t, packages)
		if bytes.Contains(encoded, []byte(":null")) {
			t.Fatalf("empty packages encode null manager arrays: %s", encoded)
		}
	})
}

func TestBuildPackageManifest(t *testing.T) {
	t.Run("invalid environment config has a root decode error", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{"type":`))
		if err == nil || provision || manifest != nil || !strings.Contains(err.Error(), "decode environment config") {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want root config decode error", manifest, provision, err)
		}
		if strings.Contains(err.Error(), "decode environment packages") {
			t.Fatalf("root config error used nested packages prefix: %v", err)
		}
	})

	t.Run("structurally invalid stored packages are a decode error", func(t *testing.T) {
		manifest, provision, err := buildPackageManifest(json.RawMessage(`{"type":"cloud","packages":{"pip":"numpy"}}`))
		if err == nil || provision || manifest != nil {
			t.Fatalf("buildPackageManifest() = (%s, %t, %v), want decode error", manifest, provision, err)
		}
		if !strings.Contains(err.Error(), "decode environment config") {
			t.Fatalf("buildPackageManifest() error = %v, want decode environment config", err)
		}
	})

	t.Run("stored dirty packages are revalidated without echoing the spec", func(t *testing.T) {
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

	t.Run("empty and absent packages skip provisioning", func(t *testing.T) {
		for _, config := range []string{
			`{"type":"cloud","packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]}}`,
			`{"type":"cloud","packages":null}`,
			`{"type":"cloud"}`,
			`{"type":"self_hosted"}`,
		} {
			manifest, provision, err := buildPackageManifest(json.RawMessage(config))
			if err != nil || provision || manifest != nil {
				t.Fatalf("buildPackageManifest(%s) = (%s, %t, %v), want (nil, false, nil)", config, manifest, provision, err)
			}
		}
	})

	t.Run("special characters remain JSON data", func(t *testing.T) {
		specs := []string{
			"@scope/package@1.2.3",
			`requests[socks] @ https://example.test/archive.whl ; python_version >= "3.11"`,
			"package name; touch /tmp/oma-package-spec-was-shell",
		}
		configJSON := mustPackageJSON(t, map[string]any{
			"type": "cloud",
			"packages": map[string]any{
				"type": "packages",
				"npm":  []string{specs[0]},
				"pip":  specs[1:],
			},
		})
		manifest, provision, err := buildPackageManifest(configJSON)
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
		provisionCommand := buildPackageProvisionCommand(config.Config{})
		if strings.Contains(provisionCommand, specs[0]) || strings.Contains(provisionCommand, specs[2]) {
			t.Fatalf("fixed provision command contains a package spec: %q", provisionCommand)
		}
		if bytes.Contains(manifest, []byte(":null")) {
			t.Fatalf("manifest contains null package manager arrays: %s", manifest)
		}
	})
}

func TestValidatePackageProvisioningResult(t *testing.T) {
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

		err = validatePackageProvisioningResult(e2bruntime.CommandResult{
			ExitCode: 0,
			Stdout:   []byte(`{"version":1,"status":"failed","category":"package_manager","manager":"npm","stage":"install","package_count":1,"duration_ms":2,"exit_code":17}`),
		})
		if err == nil || !strings.Contains(err.Error(), "exit code 0") {
			t.Fatalf("validatePackageProvisioningResult() = %v, want failed-with-zero-exit mismatch", err)
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
