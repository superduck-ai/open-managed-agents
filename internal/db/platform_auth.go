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
	UUID string
}

type PlatformAuthUserInput struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Email            string
	Name             string
	Role             string
}

type PlatformAuthUserRef struct {
	UUID string
}

type PlatformAuthWorkspaceInput struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Name             string
	CompartmentID    string
}

type PlatformAuthWorkspaceRef struct {
	UUID string
}

type PlatformAuthWorkspaceMemberInput struct {
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	WorkspaceExternalID string
	UserUUID            string
	UserExternalID      string
	WorkspaceRole       string
}

type PlatformAuthAPIKeyInput struct {
	ExternalID        string
	WorkspaceUUID     string
	KeyHash           string
	Status            string
	CreatedByUserUUID string
	Name              string
	PartialKeyHint    string
}

const (
	findPlatformAuthUserContextQuery = `
		select u.external_id AS user_external_id,
			u.organization_uuid AS org_uuid
		from users u
		where lower(u.email) = lower(:email)
		  and u.deleted_at is null
		  and exists (
			select 1
			from workspace_members wm
			where wm.organization_uuid = u.organization_uuid
			  and wm.user_uuid = u.uuid
			  and wm.deleted_at is null
		)
		order by u.added_at asc, u.uuid asc
		limit 1
	`
	resolvePlatformSessionIdentityQuery = `
		select u.organization_uuid AS organization_uuid,
			w.uuid AS workspace_uuid,
			w.external_id AS workspace_external_id,
			u.uuid AS user_uuid, u.external_id AS user_external_id,
			ak.uuid AS api_key_uuid,
			ak.external_id AS api_key_external_id
		from users u
		join lateral (
			select uuid, external_id
			from workspaces
			where organization_uuid = u.organization_uuid
			  and archived_at is null
			order by case when external_id = 'workspace_default' then 0 else 1 end,
				created_at asc, uuid asc
			limit 1
		) w on true
		join lateral (
			select uuid, external_id
			from api_keys
			where workspace_uuid = w.uuid
			  and status = 'active'
			  and (expires_at is null or expires_at > now())
			order by case when external_id = 'api_key_default' then 0 else 1 end,
				created_at asc, uuid asc
			limit 1
		) ak on true
		where u.organization_uuid = :org_uuid
		  and u.deleted_at is null
		  and (
			u.external_id = :user_id
			or u.uuid = :user_uuid
			or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = :user_id
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
	return PlatformAuthUserContext{UserExternalID: row.UserExternalID, OrgUUID: row.OrgUUID.String()}, nil
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
		returning uuid
	`, map[string]any{"name": input.Name}); err != nil {
		return PlatformAuthOrganizationRef{}, err
	}
	return PlatformAuthOrganizationRef{UUID: row.UUID.String()}, nil
}

func (tx PlatformAuthTx) InsertUser(ctx context.Context, input PlatformAuthUserInput) (PlatformAuthUserRef, error) {
	var out uuid.UUID
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "admin"
	}
	if err := namedGetContext(ctx, tx.tx, &out, `
		insert into users (uuid, external_id, organization_uuid, email, name, role)
		values (
			:uuid, :external_id, :organization_uuid,
			:email, :name, :role
		)
		returning uuid
	`, map[string]any{
		"uuid":              dbUUID(input.UUID),
		"external_id":       input.ExternalID,
		"organization_uuid": dbUUID(input.OrganizationUUID),
		"email":             input.Email,
		"name":              input.Name,
		"role":              role,
	}); err != nil {
		return PlatformAuthUserRef{}, err
	}
	return PlatformAuthUserRef{UUID: out.String()}, nil
}

func (tx PlatformAuthTx) InsertWorkspace(ctx context.Context, input PlatformAuthWorkspaceInput) (PlatformAuthWorkspaceRef, error) {
	var out uuid.UUID
	if err := namedGetContext(ctx, tx.tx, &out, `
		insert into workspaces (uuid, external_id, organization_uuid, name, compartment_id)
		values (
			:uuid, :external_id, :organization_uuid,
			:name, :compartment_id
		)
		returning uuid
	`, map[string]any{
		"uuid":              dbUUID(input.UUID),
		"external_id":       input.ExternalID,
		"organization_uuid": dbUUID(input.OrganizationUUID),
		"name":              input.Name,
		"compartment_id":    input.CompartmentID,
	}); err != nil {
		return PlatformAuthWorkspaceRef{}, err
	}
	return PlatformAuthWorkspaceRef{UUID: out.String()}, nil
}

func (tx PlatformAuthTx) InsertWorkspaceMember(ctx context.Context, input PlatformAuthWorkspaceMemberInput) error {
	workspaceRole := strings.TrimSpace(input.WorkspaceRole)
	if workspaceRole == "" {
		workspaceRole = "workspace_admin"
	}
	_, err := namedExecContext(ctx, tx.tx, `
		insert into workspace_members (
			external_id, organization_uuid, workspace_uuid, workspace_external_id,
			user_uuid, user_external_id, workspace_role
		)
		values (
			:external_id, :organization_uuid, :workspace_uuid,
			:workspace_external_id, :user_uuid, :user_external_id, :workspace_role
		)
	`, map[string]any{
		"external_id":           input.ExternalID,
		"organization_uuid":     dbUUID(input.OrganizationUUID),
		"workspace_uuid":        dbUUID(input.WorkspaceUUID),
		"workspace_external_id": input.WorkspaceExternalID,
		"user_uuid":             dbUUID(input.UserUUID),
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
		insert into api_keys (
			external_id, workspace_uuid, key_hash, status, created_by_user_uuid, name, partial_key_hint
		)
		values (
			:external_id, :workspace_uuid, :key_hash, :status,
			:created_by_user_uuid,
			:name, :partial_key_hint
		)
	`, map[string]any{
		"external_id":          input.ExternalID,
		"workspace_uuid":       dbUUID(input.WorkspaceUUID),
		"key_hash":             input.KeyHash,
		"status":               status,
		"created_by_user_uuid": dbUUID(input.CreatedByUserUUID),
		"name":                 name,
		"partial_key_hint":     input.PartialKeyHint,
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
	userID := strings.TrimSpace(input.UserUUID)
	var userUUID any
	if parsed, err := uuid.Parse(userID); err == nil && parsed != uuid.Nil {
		userUUID = parsed
	}
	if err := namedGetContext(ctx, d.sql, &row, resolvePlatformSessionIdentityQuery, map[string]any{
		"org_uuid":  dbUUID(strings.TrimSpace(input.OrgUUID)),
		"user_id":   userID,
		"user_uuid": userUUID,
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
	UserExternalID string    `db:"user_external_id"`
	OrgUUID        uuid.UUID `db:"org_uuid"`
}

type platformAuthOrganizationRefRow struct {
	UUID uuid.UUID `db:"uuid"`
}

type platformSessionIdentityRow struct {
	OrganizationUUID    uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID string    `db:"workspace_external_id"`
	UserUUID            uuid.UUID `db:"user_uuid"`
	UserExternalID      string    `db:"user_external_id"`
	APIKeyUUID          uuid.UUID `db:"api_key_uuid"`
	APIKeyExternalID    string    `db:"api_key_external_id"`
}

func (r platformSessionIdentityRow) session() platformsession.Session {
	return platformsession.Session{
		OrganizationUUID:    r.OrganizationUUID.String(),
		WorkspaceUUID:       r.WorkspaceUUID.String(),
		WorkspaceExternalID: r.WorkspaceExternalID,
		UserUUID:            r.UserUUID.String(),
		UserExternalID:      r.UserExternalID,
		APIKeyUUID:          r.APIKeyUUID.String(),
		APIKeyExternalID:    r.APIKeyExternalID,
	}
}
