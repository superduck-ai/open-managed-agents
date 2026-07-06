package platformsession

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

const DefaultTTL = 300 * 24 * time.Hour

var ErrNotFound = errors.New("platform session not found")

type CreateInput struct {
	SessionKey string
	UserUUID   string
	OrgUUID    string
	ExpiresAt  *time.Time
}

type Session struct {
	ExternalID             string     `json:"external_id"`
	OrganizationID         int64      `json:"organization_id"`
	OrganizationUUID       string     `json:"organization_uuid"`
	OrganizationExternalID string     `json:"organization_external_id"`
	WorkspaceID            int64      `json:"workspace_id"`
	WorkspaceUUID          string     `json:"workspace_uuid"`
	WorkspaceExternalID    string     `json:"workspace_external_id"`
	UserID                 int64      `json:"user_id"`
	UserExternalID         string     `json:"user_external_id"`
	APIKeyID               int64      `json:"api_key_id"`
	APIKeyExternalID       string     `json:"api_key_external_id"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
}

type Store interface {
	Save(ctx context.Context, sessionKey string, session Session) error
	Get(ctx context.Context, sessionKey string) (Session, error)
	Delete(ctx context.Context, sessionKey string) error
}

func (s Session) Principal() auth.Principal {
	return auth.Principal{
		CredentialType:            auth.CredentialTypePlatformSession,
		APIKeyID:                  s.APIKeyID,
		APIKeyExternalID:          s.APIKeyExternalID,
		OrganizationID:            s.OrganizationID,
		OrganizationUUID:          s.OrganizationUUID,
		OrganizationExternalID:    s.OrganizationExternalID,
		WorkspaceID:               s.WorkspaceID,
		WorkspaceUUID:             s.WorkspaceUUID,
		WorkspaceExternalID:       s.WorkspaceExternalID,
		UserID:                    s.UserID,
		UserExternalID:            s.UserExternalID,
		PlatformSessionExternalID: s.ExternalID,
	}
}

func (s Session) Expired(now time.Time) bool {
	return s.ExpiresAt != nil && !s.ExpiresAt.After(now)
}

func ttlUntil(expiresAt *time.Time, now time.Time) time.Duration {
	if expiresAt == nil {
		return DefaultTTL
	}
	ttl := expiresAt.Sub(now)
	if ttl <= 0 {
		return 0
	}
	return ttl
}

func storeKey(sessionKey string) string {
	return "platform:sessions:" + auth.HashSecret(strings.TrimSpace(sessionKey))
}
