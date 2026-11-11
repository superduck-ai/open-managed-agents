package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"go.yaml.in/yaml/v3"
)

type migrationResult int

const (
	migrationUnchanged migrationResult = iota
	migrationPending
	migrationApplied
)

type legacyClient struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type legacyConfig struct {
	Vault struct {
		FeishuOAuthClient legacyClient `yaml:"feishu_oauth_client"`
	} `yaml:"vault"`
}

func readLegacyClient(path string) (legacyClient, error) {
	if path == "" {
		return legacyClient{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return legacyClient{}, errors.New("legacy_config_unavailable")
	}
	defer clear(data)
	var input legacyConfig
	if err := yaml.Unmarshal(data, &input); err != nil {
		return legacyClient{}, errors.New("legacy_config_invalid")
	}
	client := input.Vault.FeishuOAuthClient
	client.ClientID, client.ClientSecret = strings.TrimSpace(client.ClientID), strings.TrimSpace(client.ClientSecret)
	if (client.ClientID == "") != (client.ClientSecret == "") {
		return legacyClient{}, errors.New("legacy_client_incomplete")
	}
	return client, nil
}

func migrate(ctx context.Context, database *db.DB, service *secrets.Service, legacy, friday legacyClient, apply bool, out io.Writer, logger *slog.Logger) error {
	version, err := database.OAuthMigrationVersion(ctx)
	if err != nil {
		return errors.New("schema_unavailable")
	}
	fmt.Fprintf(out, "schema_version=%d\n", version)
	if version > 123 {
		return errors.New("schema_unsupported")
	}
	if !apply && version < 118 {
		return errors.New("schema_expansion_required")
	}
	if version == 118 {
		if err := checkReservedProviderID(ctx, database, out); err != nil {
			return err
		}
	}
	if apply && version < 119 {
		if err := database.MigrateOAuthSchemaTo(ctx, 119); err != nil {
			return errors.New("schema_expansion_failed")
		}
		seed, err := database.GetPlatformOAuthProviderByBuiltinKey(ctx, "feishu")
		if err != nil {
			return errors.New("builtin_missing")
		}
		logger.InfoContext(ctx, "platform oauth provider audit", "actor_type", "system", "provider_uuid", seed.UUID, "provider_key", "builtin:feishu", "action", "seed", "changed_fields", []string{"builtin_key", "protected"}, "result", "success")
		version = 119
	}
	counts := map[migrationResult]int{}
	for _, kind := range []string{"platform_oauth_providers", "env_oauth_flows"} {
		after := ""
		for {
			if err := ctx.Err(); err != nil {
				return errors.New("canceled")
			}
			rows, err := database.ListOAuthClientMigrationPage(ctx, kind, after, version < 120, version >= 119)
			if err != nil {
				return errors.New("client_scan_failed")
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				if err := ctx.Err(); err != nil {
					return errors.New("canceled")
				}
				result, err := migrateClient(ctx, database, service, kind, row, apply)
				if err != nil {
					fmt.Fprintf(out, "uuid=%s failed=1 error_code=client_migration_failed\n", row.UUID)
					return errors.New("client_migration_failed")
				}
				counts[result]++
				after = row.UUID
			}
		}
	}
	if version >= 119 {
		result, err := importBuiltin(ctx, database, service, "feishu", legacy, apply, logger)
		if err != nil {
			return err
		}
		counts[result]++
	} else if legacy.ClientID != "" {
		counts[migrationPending]++
	}
	if apply {
		if err := finalizeSchema(ctx, database, version, logger); err != nil {
			return err
		}
		result, err := importBuiltin(ctx, database, service, "friday", friday, true, logger)
		if err != nil {
			return err
		}
		counts[result]++
		if err := verifyFinalClients(ctx, database, service, logger); err != nil {
			return err
		}
		fmt.Fprintln(out, "schema_version=122")
	} else if version >= 121 {
		result, err := importBuiltin(ctx, database, service, "friday", friday, false, logger)
		if err != nil {
			return err
		}
		counts[result]++
	} else if friday.ClientID != "" {
		counts[migrationPending]++
	}
	if err := ctx.Err(); err != nil {
		return errors.New("canceled")
	}
	fmt.Fprintf(out, "success=%d pending=%d skipped=%d failed=0\n", counts[migrationApplied], counts[migrationPending], counts[migrationUnchanged])
	return nil
}

func migrateClient(ctx context.Context, database *db.DB, service *secrets.Service, kind string, row db.OAuthClientMigrationRecord, apply bool) (migrationResult, error) {
	binding := platformoauth.ProviderClientIDBinding(row.ExternalID)
	if kind == "env_oauth_flows" {
		binding = platformoauth.FlowClientIDBinding(db.EnvOAuthFlow{OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, ExternalID: row.ExternalID})
	}
	if kind == "platform_oauth_providers" {
		if _, err := platformoauth.OpenClientField(ctx, service, platformoauth.ProviderSecretBinding(row.ExternalID), row.ClientSecretEnvelope); err != nil {
			return migrationUnchanged, err
		}
	}
	if row.ClientIDEnvelope != nil {
		value, err := platformoauth.OpenClientField(ctx, service, binding, row.ClientIDEnvelope)
		if err != nil {
			return migrationUnchanged, err
		}
		if row.LegacyClientID != "" && row.LegacyClientID != value {
			return migrationUnchanged, errors.New("client_snapshot_mismatch")
		}
		return migrationUnchanged, nil
	}
	if row.LegacyClientID == "" {
		return migrationUnchanged, errors.New("client_id_missing")
	}
	if !apply {
		return migrationPending, nil
	}
	envelope, err := platformoauth.SealClientField(ctx, service, binding, row.LegacyClientID)
	if err != nil {
		return migrationUnchanged, err
	}
	if err := database.BackfillOAuthClientID(ctx, kind, row, *envelope); err != nil {
		return migrationUnchanged, err
	}
	return migrationApplied, nil
}

func importBuiltin(ctx context.Context, database *db.DB, service *secrets.Service, builtinKey string, legacy legacyClient, apply bool, logger *slog.Logger) (migrationResult, error) {
	provider, err := database.GetPlatformOAuthProviderByBuiltinKey(ctx, builtinKey)
	if err != nil {
		return migrationUnchanged, errors.New("builtin_missing")
	}
	if (provider.ClientIDEnvelope == nil) != (provider.ClientSecretEnvelope == nil) {
		return migrationUnchanged, errors.New("builtin_client_incomplete")
	}
	if provider.ClientIDEnvelope != nil {
		if _, err := platformoauth.NewResolver(database, service, logger).ResolveEnvOAuthProvider(ctx, "builtin:"+builtinKey); err != nil {
			return migrationUnchanged, errors.New("builtin_decryption_failed")
		}
		return migrationUnchanged, nil
	}
	if legacy.ClientID == "" {
		return migrationUnchanged, nil
	}
	if !apply {
		return migrationPending, nil
	}
	id, err := platformoauth.SealClientField(ctx, service, platformoauth.ProviderClientIDBinding(provider.ExternalID), legacy.ClientID)
	if err != nil {
		return migrationUnchanged, errors.New("builtin_seal_failed")
	}
	secret, err := platformoauth.SealClientField(ctx, service, platformoauth.ProviderSecretBinding(provider.ExternalID), legacy.ClientSecret)
	if err != nil {
		return migrationUnchanged, errors.New("builtin_seal_failed")
	}
	provider.ClientIDEnvelope, provider.ClientSecretEnvelope = id, secret
	provider.UpdatedAt = time.Now().UTC()
	if _, err := database.UpdatePlatformOAuthProvider(ctx, provider); err != nil {
		return migrationUnchanged, errors.New("builtin_save_failed")
	}
	logger.InfoContext(ctx, "platform oauth provider audit", "actor_type", "maintenance", "provider_uuid", provider.UUID,
		"provider_key", "builtin:"+builtinKey, "action", "import", "changed_fields", []string{"client_id", "client_secret"}, "result", "success")
	return migrationApplied, nil
}

func verifyFinalClients(ctx context.Context, database *db.DB, service *secrets.Service, logger *slog.Logger) error {
	providers, err := database.ListPlatformOAuthProviders(ctx)
	if err != nil {
		return errors.New("final_schema_invalid")
	}
	resolver := platformoauth.NewResolver(database, service, logger)
	for _, provider := range providers {
		provider, err := database.GetPlatformOAuthProviderByUUID(ctx, provider.UUID)
		if err != nil {
			return errors.New("final_read_failed")
		}
		if provider.ClientIDEnvelope == nil && provider.Protected {
			continue
		}
		if _, err := resolver.ResolveEnvOAuthProvider(ctx, platformoauth.ProviderKey(provider)); err != nil {
			return errors.New("final_decryption_failed")
		}
	}
	after := ""
	for {
		rows, err := database.ListOAuthClientMigrationPage(ctx, "env_oauth_flows", after, false, true)
		if err != nil {
			return errors.New("final_flow_read_failed")
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if _, err := migrateClient(ctx, database, service, "env_oauth_flows", row, false); err != nil {
				return errors.New("final_flow_decryption_failed")
			}
			after = row.UUID
		}
	}
}

func checkReservedProviderID(ctx context.Context, database *db.DB, out io.Writer) error {
	after := ""
	for {
		rows, err := database.ListOAuthClientMigrationPage(ctx, "platform_oauth_providers", after, true, false)
		if err != nil {
			return errors.New("client_scan_failed")
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if row.ExternalID == "poap_FeishuBuiltinOAuthClient" {
				fmt.Fprintf(out, "uuid=%s failed=1 error_code=reserved_provider_id_conflict\n", row.UUID)
				return errors.New("reserved_provider_id_conflict")
			}
			after = row.UUID
		}
	}
}

func finalizeSchema(ctx context.Context, database *db.DB, version int64, logger *slog.Logger) error {
	if version < 120 {
		if err := database.MigrateOAuthSchemaTo(ctx, 120); err != nil {
			return errors.New("schema_finalization_failed")
		}
	}
	if version < 121 {
		if err := database.MigrateOAuthSchemaTo(ctx, 121); err != nil {
			return errors.New("friday_schema_expansion_failed")
		}
		seed, err := database.GetPlatformOAuthProviderByBuiltinKey(ctx, "friday")
		if err != nil {
			return errors.New("builtin_missing")
		}
		logger.InfoContext(ctx, "platform oauth provider audit", "actor_type", "system", "provider_uuid", seed.UUID, "provider_key", "builtin:friday", "action", "seed", "changed_fields", []string{"builtin_key", "protected"}, "result", "success")
	}
	if err := database.MigrateOAuthSchemaTo(ctx, 122); err != nil {
		return errors.New("schema_finalization_failed")
	}

	return nil
}
