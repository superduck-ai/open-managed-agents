package db

import (
	"context"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (d *DB) FindOrCreateUserContextByEmail(ctx context.Context, email string) (string, string, error) {
	if d == nil || d.Pool == nil {
		return "", "", ErrNotFound
	}
	normalizedEmail := normalizeLoginEmail(email)

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	defaultName := defaultPlatformUserName(normalizedEmail)
	var orgUUID string
	var userExternalID string
	err = tx.QueryRow(ctx, `
		select u.external_id, o.uuid::text
		from users u
		join organizations o on o.id = u.organization_id
		where lower(u.email) = lower($1)
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
	`, normalizedEmail).Scan(&userExternalID, &orgUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		var createErr error
		userExternalID, orgUUID, createErr = createPlatformUserOrganization(ctx, tx, normalizedEmail, defaultName)
		if createErr != nil {
			return "", "", createErr
		}
	} else if err != nil {
		return "", "", err
	} else {
		if _, err := tx.Exec(ctx, `
			update users
			set name = $2,
				updated_at = now()
			where external_id = $1
			  and name = ''
		`, userExternalID, defaultName); err != nil {
			return "", "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return userExternalID, orgUUID, nil
}

func createPlatformUserOrganization(ctx context.Context, tx pgx.Tx, email string, defaultName string) (string, string, error) {
	orgExternalID, err := ids.New("org_")
	if err != nil {
		return "", "", err
	}
	workspaceExternalID, err := ids.New("wrkspc_")
	if err != nil {
		return "", "", err
	}
	memberExternalID, err := ids.New("wmem_")
	if err != nil {
		return "", "", err
	}
	apiKeyExternalID, err := ids.New("api_key_")
	if err != nil {
		return "", "", err
	}

	var organizationID int64
	var orgUUID string
	if err := tx.QueryRow(ctx, `
		insert into organizations (external_id, name)
		values ($1, $2)
		returning id, uuid::text
	`, orgExternalID, defaultPlatformOrganizationName(email)).Scan(&organizationID, &orgUUID); err != nil {
		return "", "", err
	}

	userUUID := uuid.NewString()
	userExternalID := taggedExternalUserID(userUUID)
	var userID int64
	if err := tx.QueryRow(ctx, `
		insert into users (uuid, external_id, organization_id, email, name, role)
		values ($1, $2, $3, $4, $5, 'admin')
		returning id
	`, userUUID, userExternalID, organizationID, email, defaultName).Scan(&userID); err != nil {
		return "", "", err
	}

	workspaceUUID := uuid.NewString()
	var workspaceID int64
	if err := tx.QueryRow(ctx, `
		insert into workspaces (uuid, external_id, organization_id, name, compartment_id)
		values ($1, $2, $3, 'default', $4)
		returning id
	`, workspaceUUID, workspaceExternalID, organizationID, uuid.NewString()).Scan(&workspaceID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role
		)
		values ($1, $2, $3, $4, $5, $6, 'workspace_admin')
	`, memberExternalID, organizationID, workspaceID, workspaceExternalID, userID, userExternalID); err != nil {
		return "", "", err
	}

	rawKey := "sk-ant-api03-" + consoleRandomToken(32)
	if _, err := tx.Exec(ctx, `
		insert into api_keys (external_id, workspace_id, key_hash, status, created_by_user_id, name, partial_key_hint)
		values ($1, $2, $3, 'active', $4, $5, $6)
	`, apiKeyExternalID, workspaceID, auth.HashAPIKey(rawKey), userID, "default", partialAPIKeyHint(rawKey)); err != nil {
		return "", "", err
	}
	return userExternalID, orgUUID, nil
}

func (d *DB) ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error) {
	if d == nil || d.Pool == nil {
		return platformsession.Session{}, ErrNotFound
	}
	if strings.TrimSpace(input.SessionKey) == "" || strings.TrimSpace(input.UserUUID) == "" || strings.TrimSpace(input.OrgUUID) == "" {
		return platformsession.Session{}, ErrNotFound
	}

	var session platformsession.Session
	if err := d.Pool.QueryRow(ctx, `
			select o.id, o.uuid::text, o.external_id,
				w.id, w.uuid::text, w.external_id,
				u.id, u.external_id,
				ak.id, ak.external_id
			from organizations o
			join users u on u.organization_id = o.id
			join lateral (
				select id, uuid, external_id
				from workspaces
				where organization_id = o.id
				  and archived_at is null
				order by case when external_id = 'workspace_default' then 0 else 1 end, created_at asc, id asc
				limit 1
			) w on true
			join lateral (
				select id, external_id
				from api_keys
				where workspace_id = w.id
				  and status = 'active'
				  and (expires_at is null or expires_at > now())
				order by case when external_id = 'api_key_default' then 0 else 1 end, created_at asc, id asc
				limit 1
		) ak on true
		where (o.uuid::text = $1 or o.external_id = $1)
		  and u.deleted_at is null
			  and (
				u.external_id = $2
				or u.uuid::text = $2
				or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $2
			  )
			limit 1
		`, strings.TrimSpace(input.OrgUUID), strings.TrimSpace(input.UserUUID)).Scan(
		&session.OrganizationID, &session.OrganizationUUID, &session.OrganizationExternalID,
		&session.WorkspaceID, &session.WorkspaceUUID, &session.WorkspaceExternalID,
		&session.UserID, &session.UserExternalID,
		&session.APIKeyID, &session.APIKeyExternalID,
	); err != nil {
		return platformsession.Session{}, mapNoRows(err)
	}
	sessionUUID := uuid.NewString()
	session.ExternalID = "platform_session_" + strings.ReplaceAll(sessionUUID, "-", "")
	session.ExpiresAt = input.ExpiresAt
	return session, nil
}

func normalizeLoginEmail(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return "test@qq.com"
	}
	return normalized
}

func defaultPlatformUserName(email string) string {
	localPart, _, _ := strings.Cut(strings.TrimSpace(email), "@")
	localPart = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(localPart)
	if localPart == "" {
		return "Local User"
	}
	return localPart
}

func defaultPlatformOrganizationName(email string) string {
	name := defaultPlatformUserName(email)
	if name == "Local User" {
		return "Local Organization"
	}
	return name
}

func taggedExternalUserID(userUUID string) string {
	compact := strings.ReplaceAll(userUUID, "-", "")
	if len(compact) > 24 {
		compact = compact[:24]
	}
	return "user_" + compact
}
