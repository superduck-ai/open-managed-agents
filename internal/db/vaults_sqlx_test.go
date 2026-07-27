package db

import (
	"strings"
	"testing"
	"time"
)

func TestVaultQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 11, 0, 0, 0, time.UTC)
	vaultQuery, vaultArguments := listVaultsQuery(ListVaultsPageParams{
		WorkspaceID: 42,
		Limit:       20,
		Cursor:      &VaultPageCursor{CreatedAt: now, ID: 7},
	})
	credentialQuery, credentialArguments := listVaultCredentialsQuery(ListVaultCredentialsPageParams{
		WorkspaceID:     42,
		VaultExternalID: "vault_test",
		Limit:           20,
		Cursor:          &VaultCredentialPageCursor{CreatedAt: now, ID: 8},
	})

	t.Run("rejects a missing named argument", func(t *testing.T) {
		incompleteArguments := make(map[string]any, len(credentialArguments)-1)
		for name, value := range credentialArguments {
			if name != "cursor_id" {
				incompleteArguments[name] = value
			}
		}
		if _, _, err := bindNamed(postgresRebinder{}, credentialQuery, incompleteArguments); err == nil {
			t.Fatal("bindNamed() error = nil, want missing argument error")
		}
	})

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "list vaults",
			query:        vaultQuery,
			arguments:    vaultArguments,
			wantArgCount: 5,
		},
		{
			name:         "get vault",
			query:        vaultSelectSQL() + ` where workspace_id = :workspace_id and external_id = :external_id`,
			arguments:    vaultLookupArguments(42, "vault_test"),
			wantArgCount: 2,
		},
		{
			name:         "list credentials",
			query:        credentialQuery,
			arguments:    credentialArguments,
			wantArgCount: 6,
		},
		{
			name:         "get credential",
			query:        vaultCredentialSelectSQL() + ` where workspace_id = :workspace_id and vault_external_id = :vault_external_id and external_id = :credential_external_id`,
			arguments:    vaultCredentialLookupArguments(42, "vault_test", "cred_test"),
			wantArgCount: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains named parameter syntax: %q", query)
			}
			if strings.Contains(test.query, "::") {
				t.Fatalf("query uses PostgreSQL cast shorthand: %q", test.query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestVaultCredentialArgumentsPreserveJSONBoundaries(t *testing.T) {
	credential := VaultCredential{
		Metadata:      []byte(`{"team":"platform"}`),
		Auth:          []byte(`{"type":"bearer"}`),
		SecretPayload: []byte(`{"token":"encrypted"}`),
	}
	arguments := vaultCredentialArguments(credential)

	for _, field := range []string{"metadata", "auth", "secret_payload"} {
		if len(arguments[field].([]byte)) == 0 {
			t.Fatalf("%s argument is empty", field)
		}
	}
}
