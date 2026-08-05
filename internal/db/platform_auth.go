package db

import (
	"context"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/yourbatis"

	"github.com/google/uuid"
)

type PlatformAuthUserContext struct {
	UserExternalID string
	OrgUUID        string
}

type PlatformAuthOrganizationInput struct{ Name string }
type PlatformAuthOrganizationRef struct{ UUID string }

type PlatformAuthUserInput struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Email            string
	Name             string
	Role             string
}

type PlatformAuthUserRef struct{ UUID string }

type PlatformAuthWorkspaceInput struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Name             string
	CompartmentID    string
}

type PlatformAuthWorkspaceRef struct{ UUID string }

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

type PlatformAuthTx struct{ executor yourbatis.Executor }

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
	if d == nil || d.mapperDB == nil {
		return ErrNotFound
	}
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return fn(PlatformAuthTx{executor: executor})
	})
}

func (tx PlatformAuthTx) FindUserContextByEmail(ctx context.Context, email string) (PlatformAuthUserContext, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return PlatformAuthUserContext{}, ErrNotFound
	}
	mapper := NewPlatformAuthUserMapper(tx.executor)
	row, err := mapper.FindContextByEmail(ctx, email)
	if err != nil {
		return PlatformAuthUserContext{}, mapNoRows(err)
	}
	return PlatformAuthUserContext{UserExternalID: row.UserExternalID, OrgUUID: row.OrgUUID}, nil
}

func (tx PlatformAuthTx) UpdateEmptyUserName(ctx context.Context, userExternalID, defaultName string) error {
	mapper := NewPlatformAuthUserMapper(tx.executor)
	return mapper.UpdateEmptyName(ctx, strings.TrimSpace(userExternalID), strings.TrimSpace(defaultName))
}

func (tx PlatformAuthTx) InsertOrganization(ctx context.Context, input PlatformAuthOrganizationInput) (PlatformAuthOrganizationRef, error) {
	mapper := NewPlatformAuthOrganizationMapper(tx.executor)
	uuid, err := mapper.Insert(ctx, input.Name)
	return PlatformAuthOrganizationRef{UUID: uuid}, err
}

func (tx PlatformAuthTx) InsertUser(ctx context.Context, input PlatformAuthUserInput) (PlatformAuthUserRef, error) {
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "admin"
	}
	mapper := NewPlatformAuthUserMapper(tx.executor)
	uuid, err := mapper.Insert(ctx, insertPlatformAuthUserParams{
		UUID:             input.UUID,
		ExternalID:       input.ExternalID,
		OrganizationUUID: input.OrganizationUUID,
		Email:            input.Email,
		Name:             input.Name,
		Role:             role,
	})
	return PlatformAuthUserRef{UUID: uuid}, err
}

func (tx PlatformAuthTx) InsertWorkspace(ctx context.Context, input PlatformAuthWorkspaceInput) (PlatformAuthWorkspaceRef, error) {
	mapper := NewPlatformAuthWorkspaceMapper(tx.executor)
	uuid, err := mapper.Insert(ctx, platformAuthWorkspaceInsertParams(input))
	return PlatformAuthWorkspaceRef{UUID: uuid}, err
}

func (tx PlatformAuthTx) InsertWorkspaceMember(ctx context.Context, input PlatformAuthWorkspaceMemberInput) error {
	role := strings.TrimSpace(input.WorkspaceRole)
	if role == "" {
		role = "workspace_admin"
	}
	mapper := NewPlatformAuthWorkspaceMemberMapper(tx.executor)
	return mapper.Insert(ctx, insertPlatformAuthWorkspaceMemberParams{
		ExternalID:          input.ExternalID,
		OrganizationUUID:    input.OrganizationUUID,
		WorkspaceUUID:       input.WorkspaceUUID,
		WorkspaceExternalID: input.WorkspaceExternalID,
		UserUUID:            input.UserUUID,
		UserExternalID:      input.UserExternalID,
		WorkspaceRole:       role,
	})
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
	mapper := NewPlatformAuthAPIKeyMapper(tx.executor)
	return mapper.Insert(ctx, insertPlatformAuthAPIKeyParams{
		ExternalID:        input.ExternalID,
		WorkspaceUUID:     input.WorkspaceUUID,
		KeyHash:           input.KeyHash,
		Status:            status,
		CreatedByUserUUID: input.CreatedByUserUUID,
		Name:              name,
		PartialKeyHint:    input.PartialKeyHint,
	})
}

func (d *DB) ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error) {
	if d == nil || d.mapperDB == nil {
		return platformsession.Session{}, ErrNotFound
	}
	userID := strings.TrimSpace(input.UserUUID)
	organizationUUID := strings.TrimSpace(input.OrgUUID)
	if strings.TrimSpace(input.SessionKey) == "" || userID == "" || organizationUUID == "" {
		return platformsession.Session{}, ErrNotFound
	}
	parsedUserUUID := tryParseDBUUIDIdentifierString(userID)
	mapper := NewPlatformAuthUserMapper(d.mapperDB)
	row, err := mapper.ResolveSessionIdentity(ctx, organizationUUID, userID, optionalVaultString(parsedUserUUID))
	if err != nil {
		return platformsession.Session{}, mapNoRows(err)
	}
	session := row.session()
	sessionUUID := uuid.NewString()
	session.ExternalID = "platform_session_" + strings.ReplaceAll(sessionUUID, "-", "")
	session.ExpiresAt = input.ExpiresAt
	return session, nil
}

func platformAuthWorkspaceInsertParams(input PlatformAuthWorkspaceInput) insertPlatformAuthWorkspaceParams {
	return insertPlatformAuthWorkspaceParams{
		UUID:             input.UUID,
		ExternalID:       input.ExternalID,
		OrganizationUUID: input.OrganizationUUID,
		Name:             input.Name,
		CompartmentID:    input.CompartmentID,
	}
}

func (r platformSessionIdentityRow) session() platformsession.Session {
	return platformsession.Session{
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		WorkspaceExternalID: r.WorkspaceExternalID,
		UserUUID:            r.UserUUID,
		UserExternalID:      r.UserExternalID,
		APIKeyUUID:          r.APIKeyUUID,
		APIKeyExternalID:    r.APIKeyExternalID,
	}
}
