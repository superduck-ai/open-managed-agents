package platformauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

type Store interface {
	WithPlatformAuthTx(ctx context.Context, fn func(db.PlatformAuthTxStore) error) error
	ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error)
}

type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) FindOrCreateUserContextByEmail(ctx context.Context, email string) (string, string, error) {
	if s == nil || s.store == nil {
		return "", "", db.ErrNotFound
	}
	normalizedEmail := normalizeLoginEmail(email)
	defaultName := defaultPlatformUserName(normalizedEmail)

	var userExternalID string
	var orgUUID string
	if err := s.store.WithPlatformAuthTx(ctx, func(tx db.PlatformAuthTxStore) error {
		existing, err := tx.FindUserContextByEmail(ctx, normalizedEmail)
		if errors.Is(err, db.ErrNotFound) {
			created, createErr := createDefaultUserOrganization(ctx, tx, normalizedEmail, defaultName)
			if createErr != nil {
				return createErr
			}
			userExternalID = created.UserExternalID
			orgUUID = created.OrgUUID
			return nil
		}
		if err != nil {
			return err
		}
		userExternalID = existing.UserExternalID
		orgUUID = existing.OrgUUID
		return tx.UpdateEmptyUserName(ctx, userExternalID, defaultName)
	}); err != nil {
		return "", "", err
	}
	return userExternalID, orgUUID, nil
}

func (s *Service) ResolvePlatformSessionIdentity(ctx context.Context, input platformsession.CreateInput) (platformsession.Session, error) {
	if s == nil || s.store == nil {
		return platformsession.Session{}, db.ErrNotFound
	}
	return s.store.ResolvePlatformSessionIdentity(ctx, input)
}

func createDefaultUserOrganization(ctx context.Context, tx db.PlatformAuthTxStore, email string, defaultName string) (db.PlatformAuthUserContext, error) {
	workspaceExternalID, err := ids.New("wrkspc_")
	if err != nil {
		return db.PlatformAuthUserContext{}, err
	}
	memberExternalID, err := ids.New("wmem_")
	if err != nil {
		return db.PlatformAuthUserContext{}, err
	}
	apiKeyExternalID, err := ids.New("api_key_")
	if err != nil {
		return db.PlatformAuthUserContext{}, err
	}

	org, err := tx.InsertOrganization(ctx, db.PlatformAuthOrganizationInput{
		Name: defaultPlatformOrganizationName(email),
	})
	if err != nil {
		return db.PlatformAuthUserContext{}, err
	}

	userUUID := uuid.NewV4().String()
	userExternalID := taggedExternalUserID(userUUID)
	user, err := tx.InsertUser(ctx, db.PlatformAuthUserInput{
		UUID:             userUUID,
		ExternalID:       userExternalID,
		OrganizationUUID: org.UUID,
		Email:            email,
		Name:             defaultName,
		Role:             "admin",
	})
	if err != nil {
		return db.PlatformAuthUserContext{}, err
	}

	workspace, err := tx.InsertWorkspace(ctx, db.PlatformAuthWorkspaceInput{
		UUID:             uuid.NewV4().String(),
		ExternalID:       workspaceExternalID,
		OrganizationUUID: org.UUID,
		Name:             "default",
		CompartmentID:    uuid.NewV4().String(),
	})
	if err != nil {
		return db.PlatformAuthUserContext{}, err
	}
	if err := tx.InsertWorkspaceMember(ctx, db.PlatformAuthWorkspaceMemberInput{
		ExternalID:          memberExternalID,
		OrganizationUUID:    org.UUID,
		WorkspaceUUID:       workspace.UUID,
		WorkspaceExternalID: workspaceExternalID,
		UserUUID:            user.UUID,
		UserExternalID:      userExternalID,
		WorkspaceRole:       "workspace_admin",
	}); err != nil {
		return db.PlatformAuthUserContext{}, err
	}

	rawKey := "sk-ant-api03-" + randomToken(32)
	if err := tx.InsertAPIKey(ctx, db.PlatformAuthAPIKeyInput{
		ExternalID:        apiKeyExternalID,
		WorkspaceUUID:     workspace.UUID,
		KeyHash:           auth.HashAPIKey(rawKey),
		Status:            "active",
		CreatedByUserUUID: user.UUID,
		Name:              "default",
		PartialKeyHint:    partialAPIKeyHint(rawKey),
	}); err != nil {
		return db.PlatformAuthUserContext{}, err
	}
	return db.PlatformAuthUserContext{UserExternalID: userExternalID, OrgUUID: org.UUID}, nil
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

func randomToken(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func partialAPIKeyHint(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}
