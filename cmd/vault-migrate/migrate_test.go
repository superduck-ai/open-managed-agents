package main

import (
	"bytes"
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"io"
	"strings"
	"testing"
)

func TestCommandRejectsInvalidOptions(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "--from and --to are required"},
		{[]string{"--from", "local", "--to", "local"}, "same-provider rotation"},
		{[]string{"--from", "typo", "--to", "hashicorp_vault"}, "choose local"},
		{[]string{"--from", "local", "--to", "hashicorp_vault", "unexpected"}, "unexpected positional arguments"},
	} {
		if err := run(tc.args, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("expected %q, got %v", tc.want, err)
		}
	}
}

func TestCommandHelp(t *testing.T) {
	t.Setenv("CONFIG_FILE", "/missing/config.yaml")
	var output bytes.Buffer
	if err := run([]string{"--help"}, io.Discard, &output); err != nil {
		t.Fatalf("help must succeed without configuration: %v", err)
	}

}

func TestMigrationDocumentFormats(t *testing.T) {
	for _, tc := range []struct{ name, kind, raw, wantError string }{
		{"ambiguous provider", "vault_credentials", `{"KeyProvider":"local","key_provider":"aliyun_kms"}`, "ambiguous secret envelope fields"},
		{"missing provider", "vault_credentials", `{"ciphertext":"dGVzdA=="}`, "invalid secret envelope: missing key_provider"},
		{"mixed token", "session_resources", `{"authorization_token":"test-secret","envelope":{}}`, "legacy plaintext Git token"},
		{"unknown object", "session_resources", `{"unexpected":"test-secret"}`, "invalid Git token envelope"},
		{"null envelope", "session_resources", `{"envelope":null}`, "missing key_provider"},
		{"partial envelope", "session_resources", `{"envelope":{"key_provider":"local"}}`, "unknown envelope format"},
		{"array token", "session_resources", `[]`, "invalid Git token envelope"},
		{"nonempty deployment array", "deployments", `[{}]`, "invalid deployment secrets"},
		{"invalid deployment entry", "deployments", `{"0":{"unexpected":true}}`, "resource index 0: invalid Git token envelope"},
		{"other provider malformed payload", "vault_credentials", `{"key_provider":"aliyun_kms","ciphertext":"not-base64"}`, ""},
		{"empty resource", "session_resources", `{ }`, ""},
		{"null resource", "session_resources", ` null `, ""},
		{"empty deployment", "deployments", `{}`, ""},
		{"legacy empty deployment", "deployments", `[ ]`, ""},
		{"empty deployment entries", "deployments", `{"0":{},"1":null}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, count, err := migrateDocument(t.Context(), nil, tc.kind, db.SecretMigrationRecord{Document: json.RawMessage(tc.raw)}, "local", 0, false)
			if tc.wantError == "" {
				if err != nil || count != 0 {
					t.Fatalf("empty document: count=%d err=%v", count, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("expected safe error %q; got %v", tc.wantError, err)
			}
		})
	}
}
