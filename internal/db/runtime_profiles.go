package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const DefaultOMANativeRuntimeProfileUUID = "00000000-0000-4000-8000-000000000001"

type RuntimeProfile struct {
	ID                  int64
	UUID                string
	ExternalID          string
	Scope               string
	OrganizationUUID    string
	Name                string
	ProviderKind        string
	EndpointURL         string
	IsolationMode       string
	Config              json.RawMessage
	CapabilityOverrides json.RawMessage
	SecretEnvelope      *secrets.Envelope
	CredentialHint      string
	CredentialRevision  int64
	Status              string
	LastHealthStatus    string
	LastHealthCheckedAt *time.Time
	LastHealthError     string
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type OrganizationRuntimePolicy struct {
	OrganizationUUID           string
	DefaultRuntimeProfileUUID  string
	AllowedRuntimeProfileUUIDs []string
}

func (d *DB) ListPlatformRuntimeProfiles(ctx context.Context) ([]RuntimeProfile, error) {
	rows, err := NewRuntimeProfileMapper(d.mapperDB).ListPlatform(ctx)
	if err != nil {
		return nil, err
	}
	return decodeRuntimeProfiles(rows)
}

func (d *DB) GetRuntimeProfileByUUID(ctx context.Context, profileUUID string) (RuntimeProfile, error) {
	row, err := NewRuntimeProfileMapper(d.mapperDB).FindByUUID(ctx, profileUUID)
	if err != nil {
		return RuntimeProfile{}, mapNoRows(err)
	}
	return row.profile()
}

func (d *DB) GetRuntimeProfileByExternalID(ctx context.Context, externalID string) (RuntimeProfile, error) {
	row, err := NewRuntimeProfileMapper(d.mapperDB).FindByExternalID(ctx, externalID)
	if err != nil {
		return RuntimeProfile{}, mapNoRows(err)
	}
	return row.profile()
}

func (d *DB) CreateRuntimeProfile(ctx context.Context, profile RuntimeProfile) (RuntimeProfile, error) {
	params, err := runtimeProfileUpsertParams(profile)
	if err != nil {
		return RuntimeProfile{}, err
	}
	row, err := NewRuntimeProfileMapper(d.mapperDB).Insert(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return RuntimeProfile{}, ErrDuplicate
		}
		return RuntimeProfile{}, err
	}
	return row.profile()
}

func (d *DB) UpdateRuntimeProfile(ctx context.Context, profile RuntimeProfile) (RuntimeProfile, error) {
	params, err := runtimeProfileUpsertParams(profile)
	if err != nil {
		return RuntimeProfile{}, err
	}
	row, err := NewRuntimeProfileMapper(d.mapperDB).UpdateByUUID(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return RuntimeProfile{}, ErrDuplicate
		}
		if errors.Is(err, sql.ErrNoRows) {
			return RuntimeProfile{}, ErrVersionConflict
		}
		return RuntimeProfile{}, err
	}
	return row.profile()
}

func (d *DB) UpdateRuntimeProfileHealth(ctx context.Context, profileUUID, healthStatus, healthError string, checkedAt time.Time) error {
	rowsAffected, err := NewRuntimeProfileMapper(d.mapperDB).UpdateHealthByUUID(ctx, updateRuntimeProfileHealthParams{
		UUID:                profileUUID,
		LastHealthStatus:    healthStatus,
		LastHealthError:     healthError,
		LastHealthCheckedAt: checkedAt,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeleteRuntimeProfile(ctx context.Context, profileUUID string, expectedVersion int64) error {
	if profileUUID == DefaultOMANativeRuntimeProfileUUID {
		return ErrBuiltinRuntimeProfileProtected
	}
	rowsAffected, err := NewRuntimeProfileMapper(d.mapperDB).DeleteByUUID(ctx, profileUUID, expectedVersion)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrRuntimeProfileInUse
		}
		return err
	}
	if rowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (d *DB) GetOrganizationRuntimePolicy(ctx context.Context, organizationUUID string) (OrganizationRuntimePolicy, error) {
	row, err := NewRuntimeProfileMapper(d.mapperDB).FindOrganizationPolicy(ctx, organizationUUID)
	if err != nil {
		return OrganizationRuntimePolicy{}, mapNoRows(err)
	}
	return row.policy()
}

func (d *DB) UpdateOrganizationRuntimePolicy(
	ctx context.Context,
	organizationUUID string,
	defaultProfileUUID string,
	allowedProfileUUIDs []string,
) (OrganizationRuntimePolicy, error) {
	encodedAllowed, err := json.Marshal(append([]string{}, allowedProfileUUIDs...))
	if err != nil {
		return OrganizationRuntimePolicy{}, err
	}
	row, err := NewRuntimeProfileMapper(d.mapperDB).UpdateOrganizationPolicy(ctx, updateOrganizationRuntimePolicyParams{
		OrganizationUUID:               organizationUUID,
		DefaultRuntimeProfileUUID:      defaultProfileUUID,
		AllowedRuntimeProfileUUIDsJSON: string(encodedAllowed),
	})
	if err != nil {
		return OrganizationRuntimePolicy{}, mapNoRows(err)
	}
	return row.policy()
}

func (d *DB) GetWorkspaceRuntimeProfile(ctx context.Context, organizationUUID, workspaceUUID string) (RuntimeProfile, error) {
	row, err := NewRuntimeProfileMapper(d.mapperDB).FindByWorkspace(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return RuntimeProfile{}, mapNoRows(err)
	}
	return row.profile()
}

func runtimeProfileUpsertParams(profile RuntimeProfile) (upsertRuntimeProfileParams, error) {
	envelope, err := encodeRuntimeProfileEnvelope(profile.SecretEnvelope)
	if err != nil {
		return upsertRuntimeProfileParams{}, err
	}
	var orgUUID *string
	if profile.OrganizationUUID != "" {
		value := profile.OrganizationUUID
		orgUUID = &value
	}
	configJSON := profile.Config
	if len(configJSON) == 0 {
		configJSON = json.RawMessage(`{}`)
	}
	overridesJSON := profile.CapabilityOverrides
	if len(overridesJSON) == 0 {
		overridesJSON = json.RawMessage(`{}`)
	}
	isolationMode := profile.IsolationMode
	if isolationMode == "" {
		isolationMode = "shared_upstream"
	}
	status := profile.Status
	if status == "" {
		status = "active"
	}
	credentialRevision := profile.CredentialRevision
	if credentialRevision < 1 {
		credentialRevision = 1
	}
	return upsertRuntimeProfileParams{
		UUID:                profile.UUID,
		ExternalID:          profile.ExternalID,
		Scope:               profile.Scope,
		OrganizationUUID:    orgUUID,
		Name:                profile.Name,
		ProviderKind:        profile.ProviderKind,
		EndpointURL:         profile.EndpointURL,
		IsolationMode:       isolationMode,
		Config:              string(configJSON),
		CapabilityOverrides: string(overridesJSON),
		SecretEnvelope:      envelope,
		CredentialHint:      profile.CredentialHint,
		CredentialRevision:  credentialRevision,
		Status:              status,
		Version:             profile.Version,
		CreatedAt:           profile.CreatedAt,
		UpdatedAt:           profile.UpdatedAt,
	}, nil
}

func encodeRuntimeProfileEnvelope(envelope *secrets.Envelope) (*string, error) {
	if envelope == nil {
		return nil, nil
	}
	if err := requireCompleteSecretEnvelope(envelope); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	return &value, nil
}

func decodeRuntimeProfileEnvelope(value sql.NullString) (*secrets.Envelope, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	var envelope secrets.Envelope
	if err := json.Unmarshal([]byte(value.String), &envelope); err != nil {
		return nil, fmt.Errorf("decode runtime profile secret envelope: %w", err)
	}
	if err := requireCompleteSecretEnvelope(&envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func (r runtimeProfileRow) profile() (RuntimeProfile, error) {
	envelope, err := decodeRuntimeProfileEnvelope(r.SecretEnvelope)
	if err != nil {
		return RuntimeProfile{}, err
	}
	return RuntimeProfile{
		ID:                  r.ID,
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		Scope:               r.Scope,
		OrganizationUUID:    r.OrganizationUUID.String,
		Name:                r.Name,
		ProviderKind:        r.ProviderKind,
		EndpointURL:         r.EndpointURL,
		IsolationMode:       r.IsolationMode,
		Config:              r.Config,
		CapabilityOverrides: r.CapabilityOverrides,
		SecretEnvelope:      envelope,
		CredentialHint:      r.CredentialHint,
		CredentialRevision:  r.CredentialRevision,
		Status:              r.Status,
		LastHealthStatus:    r.LastHealthStatus,
		LastHealthCheckedAt: r.LastHealthCheckedAt,
		LastHealthError:     r.LastHealthError,
		Version:             r.Version,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}, nil
}

func decodeRuntimeProfiles(rows []runtimeProfileRow) ([]RuntimeProfile, error) {
	profiles := make([]RuntimeProfile, 0, len(rows))
	for _, row := range rows {
		profile, err := row.profile()
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (r organizationRuntimePolicyRow) policy() (OrganizationRuntimePolicy, error) {
	var allowed []string
	if err := json.Unmarshal([]byte(r.AllowedRuntimeProfileUUIDsJSON), &allowed); err != nil {
		return OrganizationRuntimePolicy{}, fmt.Errorf("decode allowed runtime profile uuids: %w", err)
	}
	if allowed == nil {
		allowed = []string{}
	}
	return OrganizationRuntimePolicy{
		OrganizationUUID:           r.OrganizationUUID,
		DefaultRuntimeProfileUUID:  r.DefaultRuntimeProfileUUID,
		AllowedRuntimeProfileUUIDs: allowed,
	}, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
