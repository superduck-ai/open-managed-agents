package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var ErrIncompleteLLMProviderSecret = errors.New("incomplete LLM provider secret envelope")

type LLMProvider struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Name             string
	BaseURL          string
	APIKeyLast4      string
	ModelIDs         []string
	SecretEnvelope   *secrets.Envelope
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (d *DB) CreateLLMProvider(ctx context.Context, provider LLMProvider) (LLMProvider, error) {
	params, err := llmProviderParams(provider)
	if err != nil {
		return LLMProvider{}, err
	}
	row, err := NewLLMProviderMapper(d.mapperDB).Insert(ctx, params)
	if isUniqueViolation(err) {
		return LLMProvider{}, ErrDuplicate
	}
	if err != nil {
		return LLMProvider{}, err
	}
	return row.provider()
}

func (d *DB) ListLLMProviders(ctx context.Context, organizationUUID, workspaceUUID string) ([]LLMProvider, error) {
	rows, err := NewLLMProviderMapper(d.mapperDB).ListByWorkspace(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	providers := make([]LLMProvider, 0, len(rows))
	for _, row := range rows {
		provider, err := row.provider()
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func (d *DB) GetLLMProvider(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (LLMProvider, error) {
	row, err := NewLLMProviderMapper(d.mapperDB).FindByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return LLMProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) UpdateLLMProvider(ctx context.Context, provider LLMProvider) (LLMProvider, error) {
	params, err := llmProviderParams(provider)
	if err != nil {
		return LLMProvider{}, err
	}
	row, err := NewLLMProviderMapper(d.mapperDB).UpdateByExternalID(ctx, params)
	if isUniqueViolation(err) {
		return LLMProvider{}, ErrDuplicate
	}
	if err != nil {
		return LLMProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) DeleteLLMProvider(ctx context.Context, organizationUUID, workspaceUUID, externalID string) error {
	rows, err := NewLLMProviderMapper(d.mapperDB).DeleteByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func llmProviderParams(provider LLMProvider) (llmProviderWriteParams, error) {
	if err := requireLLMProviderEnvelope(provider.SecretEnvelope); err != nil {
		return llmProviderWriteParams{}, err
	}
	modelIDsValue := provider.ModelIDs
	if modelIDsValue == nil {
		modelIDsValue = []string{}
	}
	modelIDs, err := json.Marshal(modelIDsValue)
	if err != nil {
		return llmProviderWriteParams{}, fmt.Errorf("encode LLM provider model IDs: %w", err)
	}
	envelope := provider.SecretEnvelope
	return llmProviderWriteParams{
		UUID:             provider.UUID,
		ExternalID:       provider.ExternalID,
		OrganizationUUID: provider.OrganizationUUID,
		WorkspaceUUID:    provider.WorkspaceUUID,
		Name:             provider.Name,
		BaseURL:          provider.BaseURL,
		APIKeyLast4:      provider.APIKeyLast4,
		ModelIDs:         modelIDs,
		Ciphertext:       envelope.Ciphertext,
		Nonce:            envelope.Nonce,
		WrappedDEK:       envelope.WrappedDEK,
		FormatVersion:    envelope.FormatVersion,
		KeyProvider:      envelope.KeyProvider,
		KeyVersion:       envelope.KeyVersion,
		CreatedAt:        provider.CreatedAt,
		UpdatedAt:        provider.UpdatedAt,
	}, nil
}

func (r llmProviderRow) provider() (LLMProvider, error) {
	modelIDs := make([]string, 0)
	if err := json.Unmarshal(r.ModelIDs, &modelIDs); err != nil {
		return LLMProvider{}, fmt.Errorf("decode LLM provider model IDs: %w", err)
	}
	return LLMProvider{
		UUID:             r.UUID,
		ExternalID:       r.ExternalID,
		OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID:    r.WorkspaceUUID,
		Name:             r.Name,
		BaseURL:          r.BaseURL,
		APIKeyLast4:      r.APIKeyLast4,
		ModelIDs:         modelIDs,
		SecretEnvelope: &secrets.Envelope{
			Ciphertext:    r.Ciphertext,
			Nonce:         r.Nonce,
			WrappedDEK:    r.WrappedDEK,
			FormatVersion: r.FormatVersion,
			KeyProvider:   r.KeyProvider,
			KeyVersion:    r.KeyVersion,
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}, nil
}

func requireLLMProviderEnvelope(envelope *secrets.Envelope) error {
	if envelope == nil || len(envelope.Ciphertext) == 0 || len(envelope.Nonce) == 0 ||
		len(envelope.WrappedDEK) == 0 || envelope.FormatVersion < 1 ||
		envelope.KeyProvider == "" || envelope.KeyVersion < 1 {
		return ErrIncompleteLLMProviderSecret
	}
	return nil
}
