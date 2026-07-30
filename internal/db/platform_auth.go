package db

import (
	"context"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type PlatformAuthUserContext struct {
	UserExternalID string
	OrgUUID        string
}

type PlatformAuthOrganizationInput struct {
	Name string
}

type PlatformAuthOrganizationRef struct {
	ID   int64
	UUID string
}

type PlatformAuthUserInput struct {
	UUID           string
	ExternalID     string
	OrganizationID int64
	Email          string
	Name           string
	Role           string
}

type PlatformAuthUserRef struct {
	ID int64
}

type PlatformAuthWorkspaceInput struct {
	UUID           string
	ExternalID     string
	OrganizationID int64
	Name           string
	CompartmentID  string
}

type PlatformAuthWorkspaceRef struct {
	ID int64
}

type PlatformAuthWorkspaceMemberInput struct {
	ExternalID          string
	OrganizationID      int64
	WorkspaceID         int64
	WorkspaceExternalID string
	UserID              int64
	UserExternalID      string
	WorkspaceRole       string
}

type PlatformAuthAPIKeyInput struct {
	ExternalID      string
	WorkspaceID     int64
	KeyHash         string
	Status          string
	CreatedByUserID int64
	Name            string
	PartialKeyHint  string
}

const (
	findPlatformAuthUserContextQuery = `
		select u.external_id AS user_external_id, CAST(o.uuid AS text) AS org_uuid
		from users u
		join organizations o on o.id = u.organization_id
		where lower(u.email) = lower(:email)
		  and u.deleted_at is null
		  and exists (
			select 1
			from workspace_members wm
			where wm.organization_id = o.id
			  and wm.user_id = u.id
			  and wm.deleted_at is null
		)
		order by u.added_at asc, u.id asc
		limit 1
	`
	resolvePlatformSessionIdentityQuery = `
		select o.id AS organization_id, CAST(o.uuid AS text) AS organization_uuid,
			w.id AS workspace_id, CAST(w.uuid AS text) AS workspace_uuid,
			w.external_id AS workspace_external_id,
			u.id AS user_id, u.external_id AS user_external_id,
			ak.id AS api_key_id, ak.external_id AS api_key_external_id
		from organizations o
		join users u on u.organization_id = o.id
		join lateral (
			select id, uuid, external_id
			from workspaces
			where organization_uuid = o.uuid
			  and archived_at is null
			order by case when external_id = 'workspace_default' then 0 else 1 end,
				created_at asc, id asc
			limit 1
		) w on true
		join lateral (
			select id, external_id
			from api_keys
			where workspace_id = w.id
			  and status = 'active'
			  and (expires_at is null or expires_at > now())
			order by case when external_id = 'api_key_default' then 0 else 1 end,
				created_at asc, id asc
			limit 1
		) ak on true
		where CAST(o.uuid AS text) = :org_uuid
		  and u.deleted_at is null
		  and (
			u.external_id = :user_uuid
			or CAST(u.uuid AS text) = :user_uuid
			or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = :user_uuid
		  )
		limit 1
	`
)

type PlatformAuthTx struct {
	tx *sqlx.Tx
}

type PlatformAuthTxStore interface {
	FindUserContextByEmail(ctx context.Context, email string) (PlatformAuthUserContext, error)
	UpdateEmptyUserName(ctx context.Context, userExternalID string, defaultName string) error
	InsertOrganization(ctx context.Context, input PlatformAuthOrganizationInput) (PlatformAuthOrganizationRef, error)
	InsertUser(ctx context.Context, input PlatformAuthUserInput) (PlatformAuthUserRef, error)
	InsertWorkspace(ctx context.Context, input PlatformAuthWorkspaceInput) (PlatformAuthWorkspaceRef, error)
	InsertWorkspaceMember(ctx context.Context, input PlatformAuthWorkspaceMemberInput) error
	InsertAPIKey(ctx context.Context, input PlatformAuthAPIKeyInput) error
}

func (d *DB) WithPlatformAuthTx(ctx context.Context, fn func(PlatformAuthTxStore) error) error {
	if d == nil || d.sql == nil {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(PlatformAuthTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (tx PlatformAuthTx) FindUserContextByEmail(ctx context.Context, email string) (PlatformAuthUserContext, error) {
	if strings.TrimSpace(email) == "" {
		return PlatformAuthUserContext{}, ErrNotFound
	}
	var row platformAuthUserContextRow
	err := namedGetContext(ctx, tx.tx, &row, findPlatformAuthUserContextQuery, map[string]any{
		"email": strings.TrimSpace(email),
	})
	if err != nil {
		return PlatformAuthUserContext{}, mapNoRows(err)
	}
	return PlatformAuthUserContext{UserExternalID: row.UserExternalID, OrgUUID: row.OrgUUID}, nil
}

func (tx PlatformAuthTx) UpdateEmptyUserName(ctx context.Context, userExternalID string, defaultName string) error {
	_, err := namedExecContext(ctx, tx.tx, `
			update users
			set name = :default_name,
				updated_at = now()
			where external_id = :user_external_id
			  and name = ''
		`, map[string]any{
		"user_external_id": strings.TrimSpace(userExternalID),
		"default_name":     strings.TrimSpace(defaultName),
	})
	return err
}

func (tx PlatformAuthTx) InsertOrganization(ctx context.Context, input PlatformAuthOrganizationInput) (PlatformAuthOrganizationRef, error) {
	var row platformAuthOrganizationRefRow
	if err := namedGetContext(ctx, tx.tx, &row, `
		insert into organizations (name)
		values (:name)
		returning id, CAST(uuid AS text) AS uuid
	`, map[string]any{"name": input.Name}); err != nil {
		return PlatformAuthOrganizationRef{}, err
	}
	return PlatformAuthOrganizationRef{ID: row.ID, UUID: row.UUID}, nil
}

func (tx PlatformAuthTx) InsertUser(ctx context.Context, input PlatformAuthUserInput) (PlatformAuthUserRef, error) {
	var out PlatformAuthUserRef
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "admin"
	}
	if err := namedGetContext(ctx, tx.tx, &out.ID, `
		insert into users (uuid, external_id, organization_id, email, name, role)
		values (:uuid, :external_id, :organization_id, :email, :name, :role)
		returning id
	`, map[string]any{
		"uuid":            input.UUID,
		"external_id":     input.ExternalID,
		"organization_id": input.OrganizationID,
		"email":           input.Email,
		"name":            input.Name,
		"role":            role,
	}); err != nil {
		return PlatformAuthUserRef{}, err
	}
	return out, nil
}

func (tx PlatformAuthTx) InsertWorkspace(ctx context.Context, input PlatformAuthWorkspaceInput) (PlatformAuthWorkspaceRef, error) {
	var out PlatformAuthWorkspaceRef
	if err := namedGetContext(ctx, tx.tx, &out.ID, `
		insert into workspaces (uuid, external_id, organization_uuid, name, compartment_id)
		select :uuid, :external_id, uuid, :name, :compartment_id
		from organizations
		where id = :organization_id
		returning id
	`, map[string]any{
		"uuid":            input.UUID,
		"external_id":     input.ExternalID,
		"organization_id": input.OrganizationID,
		"name":            input.Name,
		"compartment_id":  input.CompartmentID,
	}); err != nil {
		return PlatformAuthWorkspaceRef{}, err
	}
	return out, nil
}

func (tx PlatformAuthTx) InsertWorkspaceMember(ctx context.Context, input PlatformAuthWorkspaceMemberInput) error {
	workspaceRole := strings.TrimSpace(input.WorkspaceRole)
	if workspaceRole == "" {
		workspaceRole = "workspace_admin"
	}
	_, err := namedExecContext(ctx, tx.tx, `
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role
		)
		values (
			:external_id, :organization_id, :workspace_id, :workspace_external_id,
			:user_id, :user_external_id, :workspace_role
		)
	`, map[string]any{
		"external_id":           input.ExternalID,
		"organization_id":       input.OrganizationID,
		"workspace_id":          input.WorkspaceID,
		"workspace_external_id": input.WorkspaceExternalID,
		"user_id":               input.UserID,
		"user_external_id":      input.UserExternalID,
		"workspace_role":        workspaceRole,
	})
	return err
}

func (tx PlatformAuthTx) InsertAPIKey(ctx context.Context, input PlatformAuthAPIKeyInput) error {
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "default"
	}
	_, err := namedExecContext(ctx, tx.tx, `
		insert into api_keys (external_id, workspace_id, key_hash, status, created_by_user_id, name, partial_key_hint)
		values (
			:external_id, :workspace_id, :key_hash, :status, :created_by_user_id,
			:name, :partial_key_hint
		)
	`, map[string]any{
		"external_id":        input.ExternalID,
		"workspace_id":       input.WorkspaceID,
		"key_hash":           input.KeyHash,
		"status":             status,
		"created_by_user_id": input.CreatedByUserID,
		"name":               name,
		"partial_key_hint":   input.PartialKeyHint,
	})
	return err
}

func (d *DB) ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error) {
	if d == nil || d.sql == nil {
		return platformsession.Session{}, ErrNotFound
	}
	if strings.TrimSpace(input.SessionKey) == "" || strings.TrimSpace(input.UserUUID) == "" || strings.TrimSpace(input.OrgUUID) == "" {
		return platformsession.Session{}, ErrNotFound
	}

	var row platformSessionIdentityRow
	if err := namedGetContext(ctx, d.sql, &row, resolvePlatformSessionIdentityQuery, map[string]any{
		"org_uuid":  strings.TrimSpace(input.OrgUUID),
		"user_uuid": strings.TrimSpace(input.UserUUID),
	}); err != nil {
		return platformsession.Session{}, mapNoRows(err)
	}
	session := row.session()
	sessionUUID := uuid.NewString()
	session.ExternalID = "platform_session_" + strings.ReplaceAll(sessionUUID, "-", "")
	session.ExpiresAt = input.ExpiresAt
	return session, nil
}

type platformAuthUserContextRow struct {
	UserExternalID string `db:"user_external_id"`
	OrgUUID        string `db:"org_uuid"`
}

type platformAuthOrganizationRefRow struct {
	ID   int64  `db:"id"`
	UUID string `db:"uuid"`
}

type platformSessionIdentityRow struct {
	OrganizationID      int64  `db:"organization_id"`
	OrganizationUUID    string `db:"organization_uuid"`
	WorkspaceID         int64  `db:"workspace_id"`
	WorkspaceUUID       string `db:"workspace_uuid"`
	WorkspaceExternalID string `db:"workspace_external_id"`
	UserID              int64  `db:"user_id"`
	UserExternalID      string `db:"user_external_id"`
	APIKeyID            int64  `db:"api_key_id"`
	APIKeyExternalID    string `db:"api_key_external_id"`
}

func (r platformSessionIdentityRow) session() platformsession.Session {
	return platformsession.Session{
		OrganizationID:      r.OrganizationID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceID:         r.WorkspaceID,
		WorkspaceUUID:       r.WorkspaceUUID,
		WorkspaceExternalID: r.WorkspaceExternalID,
		UserID:              r.UserID,
		UserExternalID:      r.UserExternalID,
		APIKeyID:            r.APIKeyID,
		APIKeyExternalID:    r.APIKeyExternalID,
	}
}
