package tunnels

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestInspectCACertificateRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	validPEM, _, _ := newTestTunnelCACertificate(t)
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "too large", value: strings.Repeat("x", maxTunnelCertificatePEMBytes+1)},
		{name: "not PEM", value: "not a certificate"},
		{name: "multiple blocks", value: validPEM + validPEM},
		{name: "private key", value: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("key")}))},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := inspectCACertificate(testCase.value); err == nil {
				t.Fatal("inspectCACertificate() succeeded, want error")
			}
		})
	}
}

func TestInspectCACertificateReturnsSDKMetadata(t *testing.T) {
	t.Parallel()
	certificatePEM, certificateDER, expiresAt := newTestTunnelCACertificate(t)
	fingerprint, gotExpiresAt, err := inspectCACertificate("\n" + certificatePEM + "\n")
	if err != nil {
		t.Fatalf("inspectCACertificate() error = %v", err)
	}
	wantFingerprint := sha256.Sum256(certificateDER)
	if fingerprint != hex.EncodeToString(wantFingerprint[:]) {
		t.Fatalf("fingerprint = %q, want %x", fingerprint, wantFingerprint)
	}
	if gotExpiresAt == nil || !gotExpiresAt.Equal(expiresAt) {
		t.Fatalf("expires_at = %v, want %v", gotExpiresAt, expiresAt)
	}
}

func TestNewCertificateIDUsesTCRTNamespace(t *testing.T) {
	t.Parallel()
	service := &Service{random: strings.NewReader("0123456789abcdef")}
	certificateID, err := service.newCertificateID()
	if err != nil {
		t.Fatalf("newCertificateID() error = %v", err)
	}
	if certificateID != "tcrt_30313233343536373839616263646566" {
		t.Fatalf("newCertificateID() = %q", certificateID)
	}
}

func newTestTunnelCACertificate(t *testing.T) (string, []byte, time.Time) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test CA key: %v", err)
	}
	expiresAt := time.Date(2036, time.August, 28, 1, 2, 3, 0, time.UTC)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "OMA tunnel test CA"},
		NotBefore:             time.Date(2026, time.August, 28, 1, 2, 3, 0, time.UTC),
		NotAfter:              expiresAt,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create test CA certificate: %v", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	return string(certificatePEM), certificateDER, expiresAt
}
