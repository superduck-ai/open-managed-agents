package codesessions

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"math/big"
	"net"
	"testing"
	"time"
)

const (
	// 这是迁移前生产根使用的 Subject DER。固定原始编码而不只比较 pkix.Name，
	// 可以防止未来调整 RDN 顺序或 ASN.1 字符串类型后静默破坏旧根链构建。
	legacyUpstreamProxySubjectDERHex = "304a311c301a060355040a13134f70656e204d616e61676564204167656e7473312a3028060355040313214f70656e204d616e61676564204167656e7473204343527632204d49544d204341"
	// 测试固定私钥 D=1 的 RFC 5280 method-1 SKI。该值独立于生产计算函数，
	// 用于锁定 SHA-1(subjectPublicKey BIT STRING) 这一跨启动身份合同。
	legacyUpstreamProxySubjectKeyIDHex = "b467a3999eb5efa671de0032f08a9c9e5efda0e1"
)

func TestRenewedUpstreamProxyRootMaintainsLegacyTLSClientTrust(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	privateKey := legacyUpstreamProxyTestPrivateKey()
	legacyRoot := newLegacyUpstreamProxyTestRoot(t, privateKey, now)
	renewedAuthority, err := issueUpstreamProxyCertificateAuthority(privateKey, now)
	if err != nil {
		t.Fatalf("issue renewed CA: %v", err)
	}
	if !bytes.Equal(renewedAuthority.certificate.RawSubject, legacyRoot.RawSubject) {
		t.Fatalf("renewed root RawSubject = %x, want legacy %x", renewedAuthority.certificate.RawSubject, legacyRoot.RawSubject)
	}
	if !bytes.Equal(renewedAuthority.certificate.SubjectKeyId, legacyRoot.SubjectKeyId) {
		t.Fatalf("renewed root SKI = %x, want legacy %x", renewedAuthority.certificate.SubjectKeyId, legacyRoot.SubjectKeyId)
	}

	leaf, err := renewedAuthority.certificateForHost("api.example.com", now)
	if err != nil {
		t.Fatalf("issue renewed leaf: %v", err)
	}
	legacyRoots := x509.NewCertPool()
	legacyRoots.AddCert(legacyRoot)
	clientSide, serverSide := net.Pipe()
	deadline := time.Now().Add(5 * time.Second)
	if err := clientSide.SetDeadline(deadline); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	if err := serverSide.SetDeadline(deadline); err != nil {
		t.Fatalf("set server deadline: %v", err)
	}
	defer clientSide.Close()
	defer serverSide.Close()

	serverTLS := tls.Server(serverSide, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{*leaf},
		NextProtos:   []string{"http/1.1"},
	})
	clientTLS := tls.Client(clientSide, &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    legacyRoots,
		ServerName: "api.example.com",
		NextProtos: []string{"http/1.1"},
	})
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- serverTLS.Handshake()
	}()
	if err := clientTLS.Handshake(); err != nil {
		t.Fatalf("legacy client verify renewed leaf TLS handshake: %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("renewed MITM server TLS handshake: %v", err)
	}
}

func legacyUpstreamProxyTestPrivateKey() *ecdsa.PrivateKey {
	curve := elliptic.P256()
	privateScalar := big.NewInt(1)
	x, y := curve.ScalarBaseMult(privateScalar.Bytes())
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         privateScalar,
	}
}

func newLegacyUpstreamProxyTestRoot(t *testing.T, privateKey *ecdsa.PrivateKey, now time.Time) *x509.Certificate {
	t.Helper()
	rawSubject, err := hex.DecodeString(legacyUpstreamProxySubjectDERHex)
	if err != nil {
		t.Fatalf("decode legacy root subject: %v", err)
	}
	keyID, err := hex.DecodeString(legacyUpstreamProxySubjectKeyIDHex)
	if err != nil {
		t.Fatalf("decode legacy root SKI: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		RawSubject:            rawSubject,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(upstreamProxyRootValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          keyID,
		AuthorityKeyId:        append([]byte(nil), keyID...),
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create legacy root fixture: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse legacy root fixture: %v", err)
	}
	return certificate
}
