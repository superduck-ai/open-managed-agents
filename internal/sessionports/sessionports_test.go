package sessionports

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const testSessionUUID = "0b8f4c4e-6a1d-4f3e-9a0b-2f4d5c6e7a81"

func testSettings(t *testing.T, key byte, mutate func(*config.SessionPortsConfig)) *Settings {
	t.Helper()
	codec, err := NewSlugCodec(bytes.Repeat([]byte{key}, keyBytes))
	if err != nil {
		t.Fatal(err)
	}
	ports := config.SessionPortsConfig{DomainSuffix: "preview.example.com", URLScheme: "https", BlockedPorts: []int{9229}}
	if mutate != nil {
		mutate(&ports)
	}
	return NewWithCodec(ports, 3001, codec)
}

func TestSlugCodecRejectsForgedSlugs(t *testing.T) {
	codec, err := NewSlugCodec(bytes.Repeat([]byte{1}, keyBytes))
	if err != nil {
		t.Fatal(err)
	}
	slug, err := codec.Encode(testSessionUUID, 4000)
	if err != nil {
		t.Fatal(err)
	}
	last := slug[len(slug)-1]
	nonCanonical := slug[:len(slug)-1] + string(rune(last+1))
	for name, value := range map[string]string{
		"short":         slug[:25],
		"long":          slug + "a",
		"uppercase":     strings.ToUpper(slug),
		"not base32":    "0" + slug[1:],
		"trailing bits": nonCanonical,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(value, 4000); !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("Decode(%q) error = %v, want ErrInvalidSlug", value, err)
			}
		})
	}
	if _, err := NewSlugCodec([]byte("short")); err == nil {
		t.Fatal("accepted short key")
	}
	if _, err := codec.Encode("not-a-uuid", 4000); err == nil {
		t.Fatal("encoded invalid UUID")
	}
}

func TestSlugCodecRoundTripIsDeterministicPerKey(t *testing.T) {
	first := testSettings(t, 1, nil)
	second := testSettings(t, 2, nil)
	a, _ := first.codec.Encode(testSessionUUID, 4000)
	b, _ := first.codec.Encode(testSessionUUID, 4000)
	c, _ := second.codec.Encode(testSessionUUID, 4000)
	if a != b || a == c || len(a) != SlugLength || a != strings.ToLower(a) {
		t.Fatalf("slugs = %q %q %q", a, b, c)
	}
	decoded, err := first.codec.Decode(a, 4000)
	if err != nil || decoded != testSessionUUID {
		t.Fatalf("Decode = %q, %v", decoded, err)
	}
	other, err := second.codec.Decode(a, 4000)
	if err != nil || other == testSessionUUID {
		t.Fatalf("foreign key decoded %q, %v", other, err)
	}
}

func TestSlugIsBoundToPort(t *testing.T) {
	settings := testSettings(t, 1, nil)
	first, _ := settings.codec.Encode(testSessionUUID, 4000)
	second, _ := settings.codec.Encode(testSessionUUID, 8888)
	if first == second {
		t.Fatal("different ports produced the same slug")
	}
	address, err := settings.ParseHost("8888-" + first + ".preview.example.com")
	if err == nil && address.SessionUUID == testSessionUUID {
		t.Fatal("slug for port 4000 resolved the session on port 8888")
	}
	address, err = settings.ParseHost("8888-" + second + ".preview.example.com")
	if err != nil || address.SessionUUID != testSessionUUID || address.Port != 8888 {
		t.Fatalf("own port = %+v, %v", address, err)
	}
}

func TestSettingsRejectsBlockedAndInvalidPorts(t *testing.T) {
	settings := testSettings(t, 1, nil)
	for _, port := range []int{0, -1, 65536, 49983, 3001, 9229} {
		if settings.PortAllowed(port) {
			t.Fatalf("port %d allowed", port)
		}
		if _, err := settings.URL(testSessionUUID, port); !errors.Is(err, ErrPortBlocked) {
			t.Fatalf("URL port %d error = %v", port, err)
		}
	}
	if _, err := (&Settings{}).URL(testSessionUUID, 4000); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled URL error = %v", err)
	}
}

func TestSettingsParseHostRejectsMalformedAddresses(t *testing.T) {
	settings := testSettings(t, 1, nil)
	slug, _ := settings.codec.Encode(testSessionUUID, 4000)
	for _, host := range []string{
		"oma.example.com",
		"preview.example.com",
		"4000" + slug + ".preview.example.com",
		"04000-" + slug + ".preview.example.com",
		"x-" + slug + ".preview.example.com",
		"a.4000-" + slug + ".preview.example.com",
		"49983-" + slug + ".preview.example.com",
		"4000-" + slug[:20] + ".preview.example.com",
	} {
		if _, err := settings.ParseHost(host); err == nil {
			t.Fatalf("ParseHost(%q) accepted", host)
		}
	}
}

func TestSettingsURLRoundTripsThroughHost(t *testing.T) {
	settings := testSettings(t, 1, func(ports *config.SessionPortsConfig) {
		ports.DomainSuffix = "preview.localhost"
		ports.URLScheme = "http"
		ports.URLPort = 38080
	})
	got, err := settings.URL(testSessionUUID, 4000)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := settings.codec.Encode(testSessionUUID, 4000)
	want := "http://4000-" + slug + ".preview.localhost:38080/"
	if got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	label := "4000-" + slug
	if len(label) > 63 {
		t.Fatalf("label length %d", len(label))
	}
	address, err := settings.ParseHost(strings.ToUpper(label) + ".Preview.Localhost:38080")
	if err != nil || address.Port != 4000 || address.SessionUUID != testSessionUUID {
		t.Fatalf("ParseHost = %+v, %v", address, err)
	}
	if !settings.MatchesHost("x.preview.localhost.") || settings.MatchesHost("preview.localhost") {
		t.Fatal("host matching mismatch")
	}
}

func TestNewLoadsSlugKeyFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("c2hvcnQ="), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{SessionPorts: config.SessionPortsConfig{DomainSuffix: "preview.localhost", URLScheme: "https", SlugKeyFile: bad}}
	if _, err := New(cfg, nil); err == nil {
		t.Fatal("accepted short key file")
	}
	good := filepath.Join(dir, "good")
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, keyBytes)) + "\n"
	if err := os.WriteFile(good, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.SessionPorts.SlugKeyFile = good
	first, err := New(cfg, nil)
	if err != nil || !first.Enabled() {
		t.Fatalf("New = %v", err)
	}
	second, _ := New(cfg, nil)
	a, _ := first.URL(testSessionUUID, 4000)
	b, _ := second.URL(testSessionUUID, 4000)
	if a != b {
		t.Fatalf("stable key produced %q and %q", a, b)
	}
	disabled, err := New(config.Config{}, nil)
	if err != nil || disabled.Enabled() {
		t.Fatalf("disabled settings = %v", err)
	}
}

func TestNewBlocksEffectiveSandboxMCPPortWhenUnset(t *testing.T) {
	cfg := config.Config{SessionPorts: config.SessionPortsConfig{DomainSuffix: "preview.localhost", URLScheme: "http"}}
	settings, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings.PortAllowed(config.DefaultSandboxMCPPort) {
		t.Fatal("default sandbox MCP port allowed when sandbox_mcp_port is unset")
	}
	if _, err := settings.URL(testSessionUUID, config.DefaultSandboxMCPPort); !errors.Is(err, ErrPortBlocked) {
		t.Fatalf("URL error = %v", err)
	}
	cfg.EnvironmentRunner.SandboxMCPPort = 3100
	custom, _ := New(cfg, nil)
	if custom.PortAllowed(3100) || !custom.PortAllowed(config.DefaultSandboxMCPPort) {
		t.Fatal("custom sandbox MCP port not honoured")
	}
}
