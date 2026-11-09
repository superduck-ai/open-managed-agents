package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
)

type TelemetryDestination struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	DisplayName           *string
	EndpointURL           string
	TracesEnabled         bool
	TracesEndpointURL     string
	TracesContentMode     string
	TracesMaxContentBytes int
	WorkspaceScope        string
	WorkspaceUUIDs        []string
	HeaderNames           []string
	Headers               *secrets.Envelope
	State                 string
	DisabledReason        *string
	FailureStartedAt      *time.Time
	LastError             *string
	LastErrorAt           *time.Time
	LastSuccessAt         *time.Time
	LastTestAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (r telemetryDestinationRow) destination() (TelemetryDestination, error) {
	names := []string{}
	if err := json.Unmarshal(r.HeaderNames, &names); err != nil {
		return TelemetryDestination{}, fmt.Errorf("decode telemetry destination header names: %w", err)
	}
	destination := TelemetryDestination{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID, DisplayName: r.DisplayName,
		EndpointURL: r.EndpointURL, TracesEnabled: r.TracesEnabled, TracesEndpointURL: r.TracesEndpointURL,
		TracesContentMode: r.TracesContentMode, TracesMaxContentBytes: r.TracesMaxContentBytes, WorkspaceScope: r.WorkspaceScope,
		WorkspaceUUIDs: []string{}, HeaderNames: names, State: r.State, DisabledReason: r.DisabledReason,
		FailureStartedAt: r.FailureStartedAt, LastError: r.LastError, LastErrorAt: r.LastErrorAt, LastSuccessAt: r.LastSuccessAt,
		LastTestAt: r.LastTestAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if len(r.HeadersCiphertext) > 0 && r.HeadersFormatVersion != nil && r.HeadersKeyProvider != nil && r.HeadersKeyVersion != nil {
		destination.Headers = &secrets.Envelope{
			Ciphertext: r.HeadersCiphertext, Nonce: r.HeadersNonce, WrappedDEK: r.HeadersWrappedDEK,
			FormatVersion: *r.HeadersFormatVersion, KeyProvider: *r.HeadersKeyProvider, KeyVersion: *r.HeadersKeyVersion,
		}
	}
	return destination, nil
}

func telemetryDestinationParams(destination TelemetryDestination, now time.Time) (telemetryDestinationWriteParams, error) {
	names := destination.HeaderNames
	if names == nil {
		names = []string{}
	}
	encodedNames, err := json.Marshal(names)
	if err != nil {
		return telemetryDestinationWriteParams{}, err
	}
	params := telemetryDestinationWriteParams{
		UUID: destination.UUID, ExternalID: destination.ExternalID, OrganizationUUID: destination.OrganizationUUID,
		DisplayName: destination.DisplayName, EndpointURL: destination.EndpointURL, TracesEnabled: destination.TracesEnabled,
		TracesEndpointURL: destination.TracesEndpointURL, TracesContentMode: destination.TracesContentMode,
		TracesMaxContentBytes: destination.TracesMaxContentBytes, WorkspaceScope: destination.WorkspaceScope,
		HeaderNames: encodedNames, State: destination.State, DisabledReason: destination.DisabledReason, Now: now,
	}
	if envelope := destination.Headers; envelope != nil {
		params.HeadersCiphertext, params.HeadersNonce, params.HeadersWrappedDEK = envelope.Ciphertext, envelope.Nonce, envelope.WrappedDEK
		params.HeadersFormatVersion, params.HeadersKeyProvider, params.HeadersKeyVersion = &envelope.FormatVersion, &envelope.KeyProvider, &envelope.KeyVersion
	}
	return params, nil
}

func attachTelemetryWorkspaces(ctx context.Context, mapper TelemetryDestinationMapper, organizationUUID string, rows []telemetryDestinationRow) ([]TelemetryDestination, error) {
	destinations := make([]TelemetryDestination, 0, len(rows))
	byUUID := map[string]int{}
	uuids := make([]string, 0, len(rows))
	for _, row := range rows {
		destination, err := row.destination()
		if err != nil {
			return nil, err
		}
		byUUID[destination.UUID] = len(destinations)
		uuids = append(uuids, destination.UUID)
		destinations = append(destinations, destination)
	}
	if len(uuids) == 0 {
		return destinations, nil
	}
	links, err := mapper.ListWorkspaces(ctx, organizationUUID, uuids)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		index := byUUID[link.DestinationUUID]
		destinations[index].WorkspaceUUIDs = append(destinations[index].WorkspaceUUIDs, link.WorkspaceUUID)
	}
	return destinations, nil
}

func replaceTelemetryWorkspaces(ctx context.Context, mapper TelemetryDestinationMapper, destination TelemetryDestination) error {
	if err := mapper.DeleteWorkspaces(ctx, destination.OrganizationUUID, destination.UUID); err != nil {
		return err
	}
	for _, workspaceUUID := range destination.WorkspaceUUIDs {
		if err := mapper.InsertWorkspace(ctx, destination.OrganizationUUID, destination.UUID, workspaceUUID); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) CreateTelemetryDestination(ctx context.Context, destination TelemetryDestination, limit int) (TelemetryDestination, error) {
	var created TelemetryDestination
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewTelemetryDestinationMapper(executor)
		if _, err := mapper.LockOrganization(ctx, destination.OrganizationUUID); err != nil {
			return mapNoRows(err)
		}
		count, err := mapper.CountActive(ctx, destination.OrganizationUUID)
		if err != nil {
			return err
		}
		if count >= int64(limit) {
			return ErrLimitExceeded
		}
		params, err := telemetryDestinationParams(destination, time.Now().UTC())
		if err != nil {
			return err
		}
		row, err := mapper.Insert(ctx, params)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrDuplicate
			}
			return err
		}
		if err := replaceTelemetryWorkspaces(ctx, mapper, destination); err != nil {
			return err
		}
		destinations, err := attachTelemetryWorkspaces(ctx, mapper, destination.OrganizationUUID, []telemetryDestinationRow{row})
		if err != nil {
			return err
		}
		created = destinations[0]
		return nil
	})
	return created, err
}

func (d *DB) UpdateTelemetryDestination(ctx context.Context, destination TelemetryDestination, expectedUpdatedAt time.Time) (TelemetryDestination, error) {
	var updated TelemetryDestination
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewTelemetryDestinationMapper(executor)
		params, err := telemetryDestinationParams(destination, time.Now().UTC())
		if err != nil {
			return err
		}
		params.ExpectedUpdatedAt = expectedUpdatedAt
		row, err := mapper.Update(ctx, params)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err != nil {
			if _, exists, lookupErr := mapper.FindByExternalID(ctx, destination.OrganizationUUID, destination.ExternalID); lookupErr != nil {
				return lookupErr
			} else if exists {
				return ErrVersionConflict
			}
			return ErrNotFound
		}
		destination.UUID = row.UUID
		if err := replaceTelemetryWorkspaces(ctx, mapper, destination); err != nil {
			return err
		}
		destinations, err := attachTelemetryWorkspaces(ctx, mapper, destination.OrganizationUUID, []telemetryDestinationRow{row})
		if err != nil {
			return err
		}
		updated = destinations[0]
		return nil
	})
	return updated, err
}

func (d *DB) GetTelemetryDestination(ctx context.Context, organizationUUID, externalID string) (TelemetryDestination, error) {
	mapper := NewTelemetryDestinationMapper(d.mapperDB)
	row, found, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
	if err != nil {
		return TelemetryDestination{}, err
	}
	if !found {
		return TelemetryDestination{}, ErrNotFound
	}
	destinations, err := attachTelemetryWorkspaces(ctx, mapper, organizationUUID, []telemetryDestinationRow{row})
	if err != nil {
		return TelemetryDestination{}, err
	}
	return destinations[0], nil
}

func (d *DB) GetTelemetryDestinationByUUID(ctx context.Context, destinationUUID string) (TelemetryDestination, error) {
	row, found, err := NewTelemetryDestinationMapper(d.mapperDB).FindByUUID(ctx, destinationUUID)
	if err != nil {
		return TelemetryDestination{}, err
	}
	if !found {
		return TelemetryDestination{}, ErrNotFound
	}
	return row.destination()
}

func (d *DB) ListTelemetryDestinations(ctx context.Context, organizationUUID string, workspaceUUIDs []string, limit, offset int) ([]TelemetryDestination, error) {
	mapper := NewTelemetryDestinationMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, organizationUUID, workspaceUUIDs, limit, offset)
	if err != nil {
		return nil, err
	}
	return attachTelemetryWorkspaces(ctx, mapper, organizationUUID, rows)
}

func (d *DB) ListTelemetryDestinationsForWorkspace(ctx context.Context, organizationUUID, workspaceUUID string) ([]TelemetryDestination, error) {
	rows, err := NewTelemetryDestinationMapper(d.mapperDB).ListActiveForWorkspace(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, err
	}
	destinations := make([]TelemetryDestination, 0, len(rows))
	for _, row := range rows {
		destination, err := row.destination()
		if err != nil {
			return nil, err
		}
		destinations = append(destinations, destination)
	}
	return destinations, nil
}

func (d *DB) DeleteTelemetryDestination(ctx context.Context, organizationUUID, externalID string) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewTelemetryDestinationMapper(executor)
		row, found, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if _, err := mapper.SoftDelete(ctx, organizationUUID, externalID, time.Now().UTC()); err != nil {
			return err
		}
		return mapper.DeleteWorkspaces(ctx, organizationUUID, row.UUID)
	})
}

func (d *DB) MarkTelemetryDestinationTested(ctx context.Context, organizationUUID, externalID string, now time.Time, minInterval time.Duration) error {
	mapper := NewTelemetryDestinationMapper(d.mapperDB)
	affected, err := mapper.MarkTested(ctx, organizationUUID, externalID, now, now.Add(-minInterval))
	if err != nil {
		return err
	}
	if affected == 1 {
		return nil
	}
	if _, found, lookupErr := mapper.FindByExternalID(ctx, organizationUUID, externalID); lookupErr != nil {
		return lookupErr
	} else if found {
		return ErrLimitExceeded
	}
	return ErrNotFound
}

func (d *DB) RecordTelemetryDeliverySuccess(ctx context.Context, destinationUUID string, now time.Time) error {
	return NewTelemetryDestinationMapper(d.mapperDB).RecordDeliverySuccess(ctx, destinationUUID, now)
}

func (d *DB) RecordTelemetryDeliveryFailure(ctx context.Context, destinationUUID, failure, immediateReason string, disableAfter time.Duration, now time.Time) (bool, error) {
	disabled, err := NewTelemetryDestinationMapper(d.mapperDB).RecordDeliveryFailure(ctx, telemetryDeliveryFailureParams{
		DestinationUUID: destinationUUID, Error: failure, ImmediateReason: immediateReason,
		DisableAfterSeconds: int64(disableAfter / time.Second), Now: now,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return disabled, err
}

func (d *DB) ResolveTelemetryWorkspaces(ctx context.Context, organizationUUID string, externalIDs []string) (map[string]string, error) {
	return telemetryWorkspaceMap(ctx, externalIDs, func() ([]telemetryWorkspaceIdentityRow, error) {
		return NewTelemetryDestinationMapper(d.mapperDB).ResolveWorkspaces(ctx, organizationUUID, externalIDs)
	}, func(row telemetryWorkspaceIdentityRow) (string, string) { return row.ExternalID, row.UUID })
}

func (d *DB) TelemetryWorkspaceExternalIDs(ctx context.Context, organizationUUID string, workspaceUUIDs []string) (map[string]string, error) {
	return telemetryWorkspaceMap(ctx, workspaceUUIDs, func() ([]telemetryWorkspaceIdentityRow, error) {
		return NewTelemetryDestinationMapper(d.mapperDB).WorkspaceIdentities(ctx, organizationUUID, workspaceUUIDs)
	}, func(row telemetryWorkspaceIdentityRow) (string, string) { return row.UUID, row.ExternalID })
}

func telemetryWorkspaceMap(_ context.Context, keys []string, query func() ([]telemetryWorkspaceIdentityRow, error), pair func(telemetryWorkspaceIdentityRow) (string, string)) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	rows, err := query()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		key, value := pair(row)
		result[key] = value
	}
	return result, nil
}
