package tunnels

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type createTunnelRequest struct {
	DisplayName *string `json:"display_name"`
}

type rotateTunnelTokenRequest struct {
	Reason *string `json:"reason"`
}

type tunnelResponse struct {
	ID          string  `json:"id"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	DisplayName *string `json:"display_name"`
	Domain      string  `json:"domain"`
	Type        string  `json:"type"`
}

type tunnelPageResponse struct {
	Data     []tunnelResponse `json:"data"`
	NextPage *string          `json:"next_page"`
}

type tunnelTokenResponse struct {
	ID          string `json:"id"`
	TunnelToken string `json:"tunnel_token"`
	Type        string `json:"type"`
}

type offsetCursor struct {
	Offset int `json:"offset"`
}

type connectorToken struct {
	ID        string
	Plaintext []byte
	Hash      []byte
}

type createTunnelInput struct {
	OrganizationUUID string
	WorkspaceUUID    string
	DisplayName      *string
}

type tunnelScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
}

func responseFromTunnel(tunnel db.MCPTunnel) tunnelResponse {
	return tunnelResponse{
		ID:          tunnel.ExternalID,
		ArchivedAt:  httpapi.OptionalTime(tunnel.ArchivedAt),
		CreatedAt:   httpapi.FormatTime(tunnel.CreatedAt),
		DisplayName: tunnel.DisplayName,
		Domain:      tunnel.Domain,
		Type:        "tunnel",
	}
}

func responsesFromTunnels(tunnels []db.MCPTunnel) []tunnelResponse {
	responses := make([]tunnelResponse, 0, len(tunnels))
	for _, tunnel := range tunnels {
		responses = append(responses, responseFromTunnel(tunnel))
	}
	return responses
}

func marshalCursor(offset int) (string, error) {
	data, err := json.Marshal(offsetCursor{Offset: offset})
	if err != nil {
		return "", err
	}
	return encodeCursor(data), nil
}

func encodeCursor(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func tokenResponse(token db.MCPTunnelTokenVersion, plaintext []byte) tunnelTokenResponse {
	return tunnelTokenResponse{
		ID:          token.ExternalID,
		TunnelToken: string(plaintext),
		Type:        "tunnel_token",
	}
}

func tokenBinding(scope tunnelScope, tunnelID, tokenID string) secrets.TunnelBinding {
	return secrets.TunnelBinding{
		OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID:    scope.WorkspaceUUID,
		TunnelExternalID: tunnelID,
		TokenExternalID:  tokenID,
	}
}

func tokenVersion(token connectorToken, envelope secrets.Envelope, now time.Time) db.MCPTunnelTokenVersion {
	return db.MCPTunnelTokenVersion{
		UUID:       uuid.NewString(),
		ExternalID: token.ID,
		Version:    1,
		TokenHash:  token.Hash,
		Envelope:   &envelope,
		CreatedAt:  now,
	}
}
