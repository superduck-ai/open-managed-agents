package db

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var (
	ErrOrganizationLLMProviderExists = errors.New("organization LLM provider already exists")
	ErrIncompleteLLMProviderSecret   = errors.New("incomplete LLM provider secret envelope")
	ErrLLMProviderSlugConflict       = errors.New("LLM provider slug already exists")
)

const (
	OrganizationLLMProviderSecretWorkspace    = "organization"
	llmProviderSlugConstraint                 = "llm_providers_organization_workspace_slug_key"
	llmProvidersOrganizationDefaultConstraint = "llm_providers_organization_default_key"
)

type LLMModel struct {
	Name            string   `json:"name,omitempty"`
	ID              string   `json:"id"`
	ContextWindow   *int     `json:"context_window,omitzero"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitzero"`
	InputModalities []string `json:"input_modalities,omitempty"`
}

type LLMModels []LLMModel

func (models LLMModels) IDs() []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func (models LLMModels) WithIDs(ids []string) LLMModels {
	existing := make(map[string]LLMModel, len(models))
	for _, model := range models {
		existing[model.ID] = model
	}
	result := make(LLMModels, 0, len(ids))
	for _, id := range ids {
		model := existing[id]
		model.ID = id
		result = append(result, model)
	}
	return result
}

type LLMProvider struct {
	Models           LLMModels
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Name             string
	Slug             string
	APIBackend       string
	BaseURL          string
	APIKeyLast4      string
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
	if err != nil {
		return LLMProvider{}, mapLLMProviderWriteError(err)
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

func (d *DB) GetOrganizationLLMProvider(ctx context.Context, organizationUUID string) (LLMProvider, error) {
	row, err := NewLLMProviderMapper(d.mapperDB).FindOrganizationDefault(ctx, organizationUUID)
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
	mapper := NewLLMProviderMapper(d.mapperDB)
	var row llmProviderRow
	if provider.WorkspaceUUID == "" {
		row, err = mapper.UpdateOrganizationDefault(ctx, params)
	} else {
		row, err = mapper.UpdateByExternalID(ctx, params)
	}
	err = mapLLMProviderWriteError(err)
	if err != nil {
		return LLMProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) DeleteLLMProvider(ctx context.Context, organizationUUID, workspaceUUID, externalID string) error {
	mapper := NewLLMProviderMapper(d.mapperDB)
	var rows int64
	var err error
	if workspaceUUID == "" {
		rows, err = mapper.DeleteOrganizationDefault(ctx, organizationUUID, externalID)
	} else {
		rows, err = mapper.DeleteByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	}
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func llmProviderParams(provider LLMProvider) (llmProviderWriteParams, error) {
	if provider.APIBackend == "" {
		provider.APIBackend = "messages"
	}
	if err := requireLLMProviderEnvelope(provider.SecretEnvelope); err != nil {
		return llmProviderWriteParams{}, err
	}
	models, err := jsonv2.Marshal(provider.Models)
	if err != nil {
		return llmProviderWriteParams{}, fmt.Errorf("encode provider models: %w", err)
	}
	envelope := provider.SecretEnvelope
	return llmProviderWriteParams{
		Models:           models,
		UUID:             provider.UUID,
		ExternalID:       provider.ExternalID,
		OrganizationUUID: provider.OrganizationUUID,
		WorkspaceUUID:    nullableString(provider.WorkspaceUUID),
		Name:             provider.Name,
		Slug:             provider.Slug,
		APIBackend:       provider.APIBackend,
		BaseURL:          provider.BaseURL,
		APIKeyLast4:      provider.APIKeyLast4,
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
	models := LLMModels{}
	if err := jsonv2.Unmarshal(r.Models, &models); err != nil {
		return LLMProvider{}, fmt.Errorf("decode provider models: %w", err)
	}
	return LLMProvider{
		Models:           models,
		UUID:             r.UUID,
		ExternalID:       r.ExternalID,
		OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID:    stringFromNullable(r.WorkspaceUUID),
		Name:             r.Name,
		Slug:             r.Slug,
		APIBackend:       r.APIBackend,
		BaseURL:          r.BaseURL,
		APIKeyLast4:      r.APIKeyLast4,
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

func mapLLMProviderWriteError(err error) error {
	if isUniqueViolationOnConstraint(err, llmProvidersOrganizationDefaultConstraint) {
		return ErrOrganizationLLMProviderExists
	}
	if isUniqueViolationOnConstraint(err, llmProviderSlugConstraint) {
		return ErrLLMProviderSlugConflict
	}
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}
