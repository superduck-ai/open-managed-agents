package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyClientRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"vault: {feishu_oauth_client: {client_id: private-client}}", "vault: {feishu_oauth_client: {client_secret: private-secret}}", "vault: [invalid"} {
		path := filepath.Join(t.TempDir(), "legacy.yaml")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := readLegacyClient(path)
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unsafe validation result: %v", err)
		}
	}
}

func TestLegacyClientInput(t *testing.T) {
	for _, input := range []string{"{}", "vault: {feishu_oauth_client: {client_id: ' client ', client_secret: ' secret '}}"} {
		path := filepath.Join(t.TempDir(), "legacy.yaml")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		client, err := readLegacyClient(path)
		if err != nil {
			t.Fatal(err)
		}
		if client.ClientID != "" && (client.ClientID != "client" || client.ClientSecret != "secret") {
			t.Fatal("legacy input normalization failed")
		}
	}
	if _, err := readLegacyClient(""); err != nil {
		t.Fatal(err)
	}
}

func TestCommandReadOnlyDefaultsAndHelp(t *testing.T) {
	t.Setenv("CONFIG_FILE", "/missing")
	if err := run([]string{"--help"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--client-secret", "private-secret"}, io.Discard, io.Discard); err == nil {
		t.Fatal("CLI secret accepted")
	}
	if err := run(nil, io.Discard, io.Discard); err == nil || err.Error() != "configuration_unavailable" {
		t.Fatalf("unexpected error: %v", err)
	}
}
