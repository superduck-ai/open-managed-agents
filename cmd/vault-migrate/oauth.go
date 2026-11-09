package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type oauthMigrationDocument struct {
	ClientIDEnvelope     *secrets.Envelope `json:"client_id_envelope"`
	ClientSecretEnvelope *secrets.Envelope `json:"client_secret_envelope,omitempty"`
	ExternalID           string            `json:"external_id"`
	Version              int64             `json:"version,omitempty"`
}

func migrateOAuthDocument(ctx context.Context, service *secrets.Service, kind string, row db.SecretMigrationRecord, from string, fromVersion int64, apply bool) (json.RawMessage, int, error) {
	var document oauthMigrationDocument
	if err := json.Unmarshal(row.Document, &document); err != nil || document.ExternalID == "" {
		return nil, 0, errors.New("invalid OAuth migration document")
	}
	if kind == "platform_oauth_providers" && ((document.ClientIDEnvelope == nil) != (document.ClientSecretEnvelope == nil) || (document.ClientIDEnvelope == nil && document.ExternalID != "poap_FeishuBuiltinOAuthClient")) {
		return nil, 0, errors.New("incomplete OAuth client")
	}
	binding := platformoauth.ProviderClientIDBinding(document.ExternalID)
	if kind == "env_oauth_flows" {
		if document.ClientIDEnvelope == nil {
			return nil, 0, errors.New("missing OAuth flow client")
		}
		binding = platformoauth.FlowClientIDBinding(db.EnvOAuthFlow{OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, ExternalID: document.ExternalID})
	}
	count := 0
	for _, field := range []struct {
		envelope **secrets.Envelope
		binding  secrets.Binding
	}{
		{&document.ClientIDEnvelope, binding}, {&document.ClientSecretEnvelope, platformoauth.ProviderSecretBinding(document.ExternalID)},
	} {
		envelope := *field.envelope
		if envelope == nil {
			continue
		}
		if !validOAuthMigrationEnvelope(*envelope) {
			return nil, 0, errors.New("invalid OAuth envelope")
		}
		if envelope.KeyProvider != from || (fromVersion != 0 && envelope.KeyVersion != fromVersion) {
			continue
		}
		if !apply {
			if err := service.CheckMigration(ctx, *envelope); err != nil {
				return nil, 0, errors.New("OAuth source key unavailable")
			}
		} else {
			next, err := service.MigrateEnvelope(ctx, *envelope)
			if err != nil {
				if errors.Is(err, secrets.ErrMigrationTarget) {
					return nil, 0, fmt.Errorf("%w: OAuth envelope migration failed", secrets.ErrMigrationTarget)
				}
				return nil, 0, errors.New("OAuth source key unavailable")
			}
			if _, err := platformoauth.OpenClientField(ctx, service, field.binding, &next); err != nil {
				return nil, 0, errors.New("OAuth migrated ciphertext invalid")
			}
			*field.envelope = &next
		}
		count++
	}
	if count == 0 || !apply {
		return row.Document, count, nil
	}
	if kind == "platform_oauth_providers" {
		document.Version++
	}
	encoded, err := json.Marshal(document)
	return encoded, count, err
}

func auditOAuthRewrap(ctx context.Context, logger *slog.Logger, row db.SecretMigrationRecord, result string) {
	var document oauthMigrationDocument
	if json.Unmarshal(row.Document, &document) != nil {
		return
	}
	key := "custom:" + document.ExternalID
	if document.ExternalID == "poap_FeishuBuiltinOAuthClient" {
		key = "builtin:feishu"
	}
	code := ""
	if result == "failure" {
		code = "oauth_rewrap_failed"
	}
	logger.InfoContext(ctx, "platform oauth provider audit", "actor_type", "maintenance", "provider_uuid", row.UUID,
		"provider_key", key, "action", "rewrap", "changed_fields", []string{"client_id", "client_secret"}, "result", result, "error_code", code)
}

func validOAuthMigrationEnvelope(envelope secrets.Envelope) bool {
	return envelope.KeyProvider != "" && envelope.KeyVersion > 0 && envelope.FormatVersion == 1 && len(envelope.Nonce) == 12 && len(envelope.Ciphertext) >= 16 && len(envelope.WrappedDEK) > 0
}
