package tunnels

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const maxTunnelCertificatePEMBytes = 8 << 10

func (s *Service) CreateCertificate(
	ctx context.Context,
	scope tunnelScope,
	tunnelID string,
	caCertificatePEM string,
) (db.MCPTunnelCertificate, error) {
	tunnel, err := s.Get(ctx, scope, tunnelID)
	if err != nil {
		return db.MCPTunnelCertificate{}, err
	}
	fingerprint, expiresAt, err := inspectCACertificate(caCertificatePEM)
	if err != nil {
		return db.MCPTunnelCertificate{}, invalidRequest(err)
	}
	certificateID, err := s.newCertificateID()
	if err != nil {
		return db.MCPTunnelCertificate{}, internalError(
			"Could not generate tunnel certificate ID",
			fmt.Errorf("generate tunnel certificate ID: %w", err),
		)
	}
	created, err := s.db.CreateMCPTunnelCertificate(ctx, db.MCPTunnelCertificate{
		UUID:             uuid.NewString(),
		ExternalID:       certificateID,
		OrganizationUUID: scope.OrganizationUUID,
		TunnelUUID:       tunnel.UUID,
		TunnelExternalID: tunnel.ExternalID,
		CACertificatePEM: caCertificatePEM,
		Fingerprint:      fingerprint,
		ExpiresAt:        expiresAt,
		CreatedAt:        s.now(),
	})
	if err != nil {
		return db.MCPTunnelCertificate{}, internalError(
			"Could not create tunnel certificate",
			fmt.Errorf("create tunnel certificate: %w", err),
		)
	}
	return created, nil
}

func (s *Service) GetCertificate(
	ctx context.Context,
	scope tunnelScope,
	tunnelID string,
	certificateID string,
) (db.MCPTunnelCertificate, error) {
	tunnel, err := s.Get(ctx, scope, tunnelID)
	if err != nil {
		return db.MCPTunnelCertificate{}, err
	}
	certificate, err := s.db.GetMCPTunnelCertificate(
		ctx,
		scope.OrganizationUUID,
		tunnel.UUID,
		certificateID,
	)
	if err != nil {
		return db.MCPTunnelCertificate{}, mapCertificateLookupError(err, certificateID, "retrieve")
	}
	return certificate, nil
}

func (s *Service) ListCertificates(
	ctx context.Context,
	scope tunnelScope,
	tunnelID string,
	includeArchived bool,
	limit int,
	offset int,
) ([]db.MCPTunnelCertificate, bool, error) {
	tunnel, err := s.Get(ctx, scope, tunnelID)
	if err != nil {
		return nil, false, err
	}
	certificates, hasMore, err := s.db.ListMCPTunnelCertificatesPage(ctx, db.ListMCPTunnelCertificatesParams{
		OrganizationUUID: scope.OrganizationUUID,
		TunnelUUID:       tunnel.UUID,
		IncludeArchived:  includeArchived,
		Limit:            limit,
		Offset:           offset,
	})
	if err != nil {
		return nil, false, internalError("Could not list tunnel certificates", fmt.Errorf("list tunnel certificates: %w", err))
	}
	return certificates, hasMore, nil
}

func (s *Service) ArchiveCertificate(
	ctx context.Context,
	scope tunnelScope,
	tunnelID string,
	certificateID string,
) (db.MCPTunnelCertificate, error) {
	tunnel, err := s.Get(ctx, scope, tunnelID)
	if err != nil {
		return db.MCPTunnelCertificate{}, err
	}
	certificate, err := s.db.ArchiveMCPTunnelCertificate(
		ctx,
		scope.OrganizationUUID,
		tunnel.UUID,
		certificateID,
	)
	if err != nil {
		return db.MCPTunnelCertificate{}, mapCertificateLookupError(err, certificateID, "archive")
	}
	return certificate, nil
}

func (s *Service) newCertificateID() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := io.ReadFull(s.random, randomBytes); err != nil {
		return "", err
	}
	return "tcrt_" + hex.EncodeToString(randomBytes), nil
}

func inspectCACertificate(value string) (string, *time.Time, error) {
	certificatePEM := []byte(value)
	if len(certificatePEM) == 0 {
		return "", nil, errors.New("ca_certificate_pem is required")
	}
	if len(certificatePEM) > maxTunnelCertificatePEMBytes {
		return "", nil, fmt.Errorf("ca_certificate_pem must be at most %d bytes", maxTunnelCertificatePEMBytes)
	}
	trimmed := bytes.TrimSpace(certificatePEM)
	if !bytes.HasPrefix(trimmed, []byte("-----BEGIN CERTIFICATE-----")) {
		return "", nil, errors.New("ca_certificate_pem must contain exactly one PEM certificate")
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return "", nil, errors.New("ca_certificate_pem must contain exactly one PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", nil, fmt.Errorf("ca_certificate_pem contains an invalid X.509 certificate: %w", err)
	}
	if !certificate.IsCA {
		return "", nil, errors.New("ca_certificate_pem must contain a CA certificate")
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	expiresAt := certificate.NotAfter.UTC()
	return hex.EncodeToString(fingerprint[:]), &expiresAt, nil
}
