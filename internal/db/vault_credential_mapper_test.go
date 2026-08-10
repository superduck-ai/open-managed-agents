package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestVaultCredentialMapperBuilders(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC)
	creatorUUID := "33333333-3333-4333-8333-333333333333"
	formatVersion := int32(1)
	keyProvider := "local"
	keyVersion := int64(1)
	params := insertVaultCredentialParams{
		UUID:                "credential-uuid",
		ExternalID:          "vaultcred_test",
		OrganizationUUID:    "organization-uuid",
		WorkspaceUUID:       "workspace-uuid",
		VaultUUID:           "vault-uuid",
		VaultExternalID:     "vault_test",
		CreatedByAPIKeyUUID: &creatorUUID,
		DisplayName:         "Credential",
		Metadata:            []byte(`{"team":"platform"}`),
		AuthType:            "bearer",
		CredentialKey:       "token",
		Auth:                []byte(`{"type":"bearer"}`),
		Ciphertext:          []byte("cipher"),
		Nonce:               []byte("nonce"),
		WrappedDEK:          []byte("wrapped"),
		FormatVersion:       &formatVersion,
		KeyProvider:         &keyProvider,
		KeyVersion:          &keyVersion,
		Version:             0,
		CreatedAt:           createdAt,
	}
	bound := buildVaultCredentialMapperInsert(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: vaultCredentialMapperInsertStatement,
		bound:     bound,
		wantID:    "VaultCredentialMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
			"params.VaultUUID", "params.VaultExternalID", "params.CreatedByAPIKeyUUID",
			"params.DisplayName", "params.Metadata", "params.AuthType", "params.CredentialKey",
			"params.Auth", "params.Ciphertext", "params.Nonce", "params.WrappedDEK",
			"params.FormatVersion", "params.KeyProvider", "params.KeyVersion", "params.Version",
			"params.CreatedAt", "params.CreatedAt",
		},
		wantSensitiveArgumentNames: []string{"params.CredentialKey", "params.Auth", "params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
		wantSQLFragments:           []string{"INSERT INTO vault_credentials", "CAST($9 AS jsonb)", "CAST($12 AS jsonb)"},
	})

	cursor := &VaultCredentialPageCursor{CreatedAt: createdAt, UUID: "cursor-uuid"}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: vaultCredentialMapperListPageStatement,
		bound: buildVaultCredentialMapperListPage(yourbatis.DialectPostgres, listVaultCredentialsMapperParams{
			WorkspaceUUID: "workspace-uuid", VaultExternalID: "vault_test", Limit: 21, Cursor: cursor,
		}),
		wantID:   "VaultCredentialMapper.ListPage",
		wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{
			"params.WorkspaceUUID", "params.VaultExternalID", "params.Cursor.CreatedAt",
			"params.Cursor.UUID", "params.Limit",
		},
		wantSQLFragments: []string{"vault_external_id = $2", "(created_at, uuid) < ($3, $4)", "LIMIT $5"},
	})
}
