package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"time"
)

const tunnelsBeta = "mcp-tunnels-2026-05-19"

type parsedCertificate struct {
	Fingerprint string
	ExpiresAt   *time.Time
}

func newTunnelToken() (tokenID, token string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = base64.StdEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	tokenID = "ttkn_" + hex.EncodeToString(sum[:])
	return tokenID, token, nil
}

func parseCACertificatePEM(raw string) (parsedCertificate, error) {
	if strings.Contains(raw, "PRIVATE KEY") {
		return parsedCertificate{}, errors.New("ca_certificate_pem must not contain private-key material")
	}
	rest := []byte(raw)
	var certBlock *pem.Block
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			if strings.TrimSpace(string(rest)) != "" {
				return parsedCertificate{}, errors.New("ca_certificate_pem must contain exactly one certificate")
			}
			break
		}
		if block.Type != "CERTIFICATE" {
			return parsedCertificate{}, errors.New("ca_certificate_pem must contain exactly one certificate")
		}
		if certBlock != nil {
			return parsedCertificate{}, errors.New("ca_certificate_pem must contain exactly one certificate")
		}
		certBlock = block
		rest = remaining
	}
	if certBlock == nil {
		return parsedCertificate{}, errors.New("ca_certificate_pem must contain exactly one certificate")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return parsedCertificate{}, errors.New("ca_certificate_pem must contain a valid X.509 certificate")
	}
	sum := sha256.Sum256(cert.Raw)
	expiresAt := cert.NotAfter.UTC()
	return parsedCertificate{Fingerprint: hex.EncodeToString(sum[:]), ExpiresAt: &expiresAt}, nil
}

func hasTunnelsBetaValues(values []string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == tunnelsBeta {
				return true
			}
		}
	}
	return false
}
