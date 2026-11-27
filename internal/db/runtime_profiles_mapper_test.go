package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
)

func TestRuntimeProfileMapperBuilders(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	envelope := `{"ciphertext":"YWJj","nonce":"ZGVm","wrapped_dek":"Z2hp","format_version":1,"key_provider":"local","key_version":1}`
	params := upsertRuntimeProfileParams{
		UUID:                "22222222-2222-4222-8222-222222222222",
		ExternalID:          "rprof_qoder_shared",
		Scope:               "platform",
		Name:                "Qoder Cloud CN",
		ProviderKind:        "qoder_cloud",
		EndpointURL:         "https://qoder.example.test/api/v1/cloud",
		IsolationMode:       "shared_upstream",
		Config:              `{"region":"cn"}`,
		CapabilityOverrides: `{}`,
		SecretEnvelope:      &envelope,
		CredentialHint:      "sk-...1234",
		CredentialRevision:  1,
		Status:              "active",
		Version:             1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeProfileMapperInsertStatement,
		bound:     buildRuntimeProfileMapperInsert(yourbatis.DialectPostgres, params),
		wantID:    "RuntimeProfileMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.Scope", "params.OrganizationUUID",
			"params.Name", "params.ProviderKind", "params.EndpointURL", "params.IsolationMode",
			"params.Config", "params.CapabilityOverrides", "params.SecretEnvelope",
			"params.CredentialHint", "params.CredentialRevision", "params.Status",
			"params.CreatedAt", "params.UpdatedAt",
		},
		wantSensitiveArgumentNames: []string{"params.SecretEnvelope"},
		wantSQLFragments:           []string{"INSERT INTO runtime_profiles", "CAST($11 AS jsonb)", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeProfileMapperListPlatformStatement,
		bound:             buildRuntimeProfileMapperListPlatform(yourbatis.DialectPostgres),
		wantID:            "RuntimeProfileMapper.ListPlatform",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{},
		wantSQLFragments:  []string{"FROM runtime_profiles", "WHERE scope = 'platform'"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeProfileMapperFindByUUIDStatement,
		bound:             buildRuntimeProfileMapperFindByUUID(yourbatis.DialectPostgres, params.UUID),
		wantID:            "RuntimeProfileMapper.FindByUUID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"profileUUID"},
		wantSQLFragments:  []string{"WHERE uuid = $1"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeProfileMapperFindByExternalIDStatement,
		bound:             buildRuntimeProfileMapperFindByExternalID(yourbatis.DialectPostgres, params.ExternalID),
		wantID:            "RuntimeProfileMapper.FindByExternalID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"externalID"},
		wantSQLFragments:  []string{"WHERE external_id = $1"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeProfileMapperUpdateByUUIDStatement,
		bound:     buildRuntimeProfileMapperUpdateByUUID(yourbatis.DialectPostgres, params),
		wantID:    "RuntimeProfileMapper.UpdateByUUID",
		wantKind:  yourbatis.StatementUpdate,
		wantArgumentNames: []string{
			"params.Name", "params.EndpointURL", "params.IsolationMode",
			"params.Config", "params.CapabilityOverrides", "params.SecretEnvelope",
			"params.CredentialHint", "params.CredentialRevision", "params.Status",
			"params.UpdatedAt", "params.UUID", "params.Version",
		},
		wantSensitiveArgumentNames: []string{"params.SecretEnvelope"},
		wantSQLFragments:           []string{"UPDATE runtime_profiles", "version = version + 1", "WHERE uuid = $11", "AND version = $12", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeProfileMapperUpdateHealthByUUIDStatement,
		bound: buildRuntimeProfileMapperUpdateHealthByUUID(yourbatis.DialectPostgres, updateRuntimeProfileHealthParams{
			UUID:                params.UUID,
			LastHealthStatus:    "healthy",
			LastHealthError:     "",
			LastHealthCheckedAt: now,
		}),
		wantID:            "RuntimeProfileMapper.UpdateHealthByUUID",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.LastHealthStatus", "params.LastHealthError", "params.LastHealthCheckedAt", "params.UUID"},
		wantSQLFragments:  []string{"UPDATE runtime_profiles", "last_health_status = $1"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeProfileMapperDeleteByUUIDStatement,
		bound:             buildRuntimeProfileMapperDeleteByUUID(yourbatis.DialectPostgres, params.UUID, 2),
		wantID:            "RuntimeProfileMapper.DeleteByUUID",
		wantKind:          yourbatis.StatementDelete,
		wantArgumentNames: []string{"profileUUID", "expectedVersion"},
		wantSQLFragments:  []string{"DELETE FROM runtime_profiles", "uuid = $1", "version = $2"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeProfileMapperFindOrganizationPolicyStatement,
		bound:             buildRuntimeProfileMapperFindOrganizationPolicy(yourbatis.DialectPostgres, "org-1"),
		wantID:            "RuntimeProfileMapper.FindOrganizationPolicy",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID"},
		wantSQLFragments:  []string{"default_runtime_profile_uuid", "allowed_runtime_profile_uuids", "FROM organizations"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeProfileMapperUpdateOrganizationPolicyStatement,
		bound: buildRuntimeProfileMapperUpdateOrganizationPolicy(yourbatis.DialectPostgres, updateOrganizationRuntimePolicyParams{
			OrganizationUUID:               "org-1",
			DefaultRuntimeProfileUUID:      DefaultOMANativeRuntimeProfileUUID,
			AllowedRuntimeProfileUUIDsJSON: `["00000000-0000-4000-8000-000000000001"]`,
		}),
		wantID:            "RuntimeProfileMapper.UpdateOrganizationPolicy",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.DefaultRuntimeProfileUUID", "params.AllowedRuntimeProfileUUIDsJSON", "params.OrganizationUUID"},
		wantSQLFragments:  []string{"UPDATE organizations", "default_runtime_profile_uuid = $1", "jsonb_array_elements_text", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeProfileMapperFindByWorkspaceStatement,
		bound:             buildRuntimeProfileMapperFindByWorkspace(yourbatis.DialectPostgres, "org-1", "ws-1"),
		wantID:            "RuntimeProfileMapper.FindByWorkspace",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID"},
		wantSQLFragments:  []string{"FROM workspaces w", "JOIN runtime_profiles rp ON rp.uuid = w.runtime_profile_uuid"},
	})
}

func TestRuntimeProfileRowMappingAndDBMethods(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	validEnv := secrets.Envelope{
		Ciphertext:    []byte("cipher"),
		Nonce:         []byte("nonce1234567"),
		WrappedDEK:    []byte("wrapped"),
		FormatVersion: 1,
		KeyProvider:   "local",
		KeyVersion:    1,
	}
	encodedEnv, err := json.Marshal(validEnv)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	row := runtimeProfileRow{
		ID:                  1,
		UUID:                "22222222-2222-4222-8222-222222222222",
		ExternalID:          "rprof_qoder_shared",
		Scope:               "platform",
		Name:                "Qoder Cloud CN",
		ProviderKind:        "qoder_cloud",
		EndpointURL:         "https://qoder.example.test",
		IsolationMode:       "shared_upstream",
		Config:              json.RawMessage(`{"region":"cn"}`),
		CapabilityOverrides: json.RawMessage(`{}`),
		SecretEnvelope:      sql.NullString{String: string(encodedEnv), Valid: true},
		CredentialHint:      "sk-...1234",
		CredentialRevision:  2,
		Status:              "active",
		LastHealthStatus:    "healthy",
		Version:             1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	profile, err := row.profile()
	if err != nil || profile.SecretEnvelope == nil || profile.SecretEnvelope.KeyProvider != "local" {
		t.Fatalf("row.profile() = (%+v, %v)", profile, err)
	}

	policyRow := organizationRuntimePolicyRow{
		OrganizationUUID:               "org-1",
		DefaultRuntimeProfileUUID:      DefaultOMANativeRuntimeProfileUUID,
		AllowedRuntimeProfileUUIDsJSON: `["00000000-0000-4000-8000-000000000001","22222222-2222-4222-8222-222222222222"]`,
	}
	policy, err := policyRow.policy()
	if err != nil || len(policy.AllowedRuntimeProfileUUIDs) != 2 {
		t.Fatalf("policyRow.policy() = (%+v, %v)", policy, err)
	}

	if !isForeignKeyViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("expected foreign key violation to be detected")
	}

	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: runtimeProfileTestColumns(),
		rows: [][]driver.Value{
			runtimeProfileTestRow(row.UUID, row.ExternalID, string(encodedEnv), now),
		},
	})
	found, err := NewRuntimeProfileMapper(executor).FindByUUID(context.Background(), row.UUID)
	if err != nil || found.ExternalID != row.ExternalID {
		t.Fatalf("FindByUUID() = (%+v, %v)", found, err)
	}

	var emptyDB DB
	if err := emptyDB.DeleteRuntimeProfile(context.Background(), DefaultOMANativeRuntimeProfileUUID, 1); !errors.Is(err, ErrBuiltinRuntimeProfileProtected) {
		t.Fatalf("DeleteRuntimeProfile(builtin) err = %v, want ErrBuiltinRuntimeProfileProtected", err)
	}
}

func runtimeProfileTestColumns() []string {
	return []string{
		"id", "uuid", "external_id", "scope", "organization_uuid", "name", "provider_kind",
		"endpoint_url", "isolation_mode", "config", "capability_overrides", "secret_envelope",
		"credential_hint", "credential_revision", "status", "last_health_status",
		"last_health_checked_at", "last_health_error", "version", "created_at", "updated_at",
	}
}

func runtimeProfileTestRow(profileUUID, externalID, secretEnvelope string, now time.Time) []driver.Value {
	return []driver.Value{
		int64(1), profileUUID, externalID, "platform", nil, "Qoder Cloud CN", "qoder_cloud",
		"https://qoder.example.test", "shared_upstream", []byte(`{}`), []byte(`{}`), secretEnvelope,
		"sk-...1234", int64(1), "active", "unknown", nil, "", int64(1), now, now,
	}
}
