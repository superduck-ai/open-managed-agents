package tunnels

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type createTunnelCertificateRequest struct {
	CACertificatePEM string `json:"ca_certificate_pem"`
}

type tunnelCertificateResponse struct {
	ID          string  `json:"id"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   *string `json:"expires_at"`
	Fingerprint string  `json:"fingerprint"`
	TunnelID    string  `json:"tunnel_id"`
	Type        string  `json:"type"`
}

type tunnelCertificatePageResponse struct {
	Data     []tunnelCertificateResponse `json:"data"`
	NextPage *string                     `json:"next_page"`
}

func responseFromTunnelCertificate(certificate db.MCPTunnelCertificate) tunnelCertificateResponse {
	return tunnelCertificateResponse{
		ID:          certificate.ExternalID,
		ArchivedAt:  httpapi.OptionalTime(certificate.ArchivedAt),
		CreatedAt:   httpapi.FormatTime(certificate.CreatedAt),
		ExpiresAt:   httpapi.OptionalTime(certificate.ExpiresAt),
		Fingerprint: certificate.Fingerprint,
		TunnelID:    certificate.TunnelExternalID,
		Type:        "tunnel_certificate",
	}
}

func responsesFromTunnelCertificates(certificates []db.MCPTunnelCertificate) []tunnelCertificateResponse {
	responses := make([]tunnelCertificateResponse, 0, len(certificates))
	for _, certificate := range certificates {
		responses = append(responses, responseFromTunnelCertificate(certificate))
	}
	return responses
}
