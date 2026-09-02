package platformauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"uuid"

	"github.com/redis/go-redis/v9"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type Store interface {
	WithPlatformAuthTx(ctx context.Context, fn func(db.PlatformAuthTxStore) error) error
}

type EmailProvider struct {
	store         Store
	codes         EmailCodeStore
	sender        LoginCodeSender
	codeHMACKey   []byte
	acceptAnyCode bool
	logger        *slog.Logger
}

func New(cfg config.AuthConfig, store Store, redisClient *redis.Client, logger *slog.Logger) *EmailProvider {
	if cfg.SMTP.Addr == "" {
		logger = logging.LoggerOrDefault(logger)
		logger.Warn("SMTP is not configured; email login accepts any non-empty verification code")
		return &EmailProvider{store: store, acceptAnyCode: true, logger: logger}
	}
	key := sha256.Sum256([]byte("open-managed-agents/email-login-code/v1\x00" + cfg.SMTP.Password))
	return NewEmailProvider(store, newRedisEmailCodeStore(redisClient), newSMTPSender(cfg.SMTP), key[:], logger)
}

func NewEmailProvider(store Store, codes EmailCodeStore, sender LoginCodeSender, codeHMACKey []byte, logger *slog.Logger) *EmailProvider {
	return &EmailProvider{
		store:       store,
		codes:       codes,
		sender:      sender,
		codeHMACKey: bytes.Clone(codeHMACKey),
		logger:      logging.LoggerOrDefault(logger),
	}
}

func (s *EmailProvider) findOrCreateUserContextByEmail(ctx context.Context, normalizedEmail string) (string, string, error) {
	if s == nil || s.store == nil {
		return "", "", db.ErrNotFound
	}
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
