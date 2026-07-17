package codesessions

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"golang.org/x/net/websocket"
)

func TestLoadUpstreamProxyCertificateAuthorityRejectsInvalidPrivateKey(t *testing.T) {
	t.Parallel()

	unsupportedBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("not a private key"),
	})
	for name, keyPEM := range map[string][]byte{
		"malformed PEM":         []byte("not a private key"),
		"unsupported PEM block": unsupportedBlock,
		"multiple PEM blocks":   append(append([]byte(nil), unsupportedBlock...), unsupportedBlock...),
		"trailing content":      append(append([]byte(nil), unsupportedBlock...), []byte("trailing")...),
		"encrypted PEM block": pem.EncodeToMemory(&pem.Block{
			Type:    "EC PRIVATE KEY",
			Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"},
			Bytes:   []byte("encrypted"),
		}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := writeTestUpstreamProxyCA(t, "invalid-key")
			if err := os.WriteFile(files.keyFile, keyPEM, 0o600); err != nil {
				t.Fatalf("overwrite test CA key: %v", err)
			}
			if _, err := loadOrGenerateUpstreamProxyCA(files.config()); err == nil {
				t.Fatal("loadOrGenerateUpstreamProxyCA() error = nil, want invalid private key error")
			}
		})
	}
}

func TestUpstreamProxyCertificateAuthorityIgnoresDormantPrivateKey(t *testing.T) {
	t.Parallel()

	missingKeyFile := filepath.Join(t.TempDir(), "missing-key.pem")
	handler := NewHandler(config.Config{CodeSessionUpstreamProxyCAKeyFile: missingKeyFile}, NewService(nil))
	authority, err := handler.loadUpstreamProxyCA()
	if err != nil {
		t.Fatalf("loadUpstreamProxyCA() error = %v", err)
	}
	if authority == nil || authority.certificate == nil {
		t.Fatal("loadUpstreamProxyCA() returned no temporary certificate authority")
	}
}

func TestUpstreamProxyMITMTransportBoundsResponseHeaderWait(t *testing.T) {
	t.Parallel()

	transport := newUpstreamProxyMITMTransport(&upstreamProxyMITMTLSDialer{})
	if transport.ResponseHeaderTimeout != upstreamProxyMITMHeaderTimeout {
		t.Fatalf(
			"ResponseHeaderTimeout = %s, want %s",
			transport.ResponseHeaderTimeout,
			upstreamProxyMITMHeaderTimeout,
		)
	}
}

func TestParseUpstreamProxyCAPrivateKeySupportsDeploymentPEMFormats(t *testing.T) {
	t.Parallel()

	ecPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC private key: %v", err)
	}
	sec1DER, err := x509.MarshalECPrivateKey(ecPrivateKey)
	if err != nil {
		t.Fatalf("marshal SEC1 private key: %v", err)
	}
	pkcs8DER, err := x509.MarshalPKCS8PrivateKey(ecPrivateKey)
	if err != nil {
		t.Fatalf("marshal PKCS#8 private key: %v", err)
	}
	rsaPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA private key: %v", err)
	}

	for name, keyPEM := range map[string][]byte{
		"SEC1 EC": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1DER}),
		"PKCS#8":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8DER}),
		"PKCS#1 RSA": pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsaPrivateKey),
		}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseUpstreamProxyCAPrivateKey(keyPEM); err != nil {
				t.Fatalf("parseUpstreamProxyCAPrivateKey() error = %v", err)
			}
		})
	}
}

func TestUpstreamProxyMITMRejectsServerNameDifferentFromConnectTarget(t *testing.T) {
	t.Parallel()

	files := writeTestUpstreamProxyCA(t, "sni")
	authority, err := loadOrGenerateUpstreamProxyCA(files.config())
	if err != nil {
		t.Fatalf("load test CA: %v", err)
	}
	tlsConfig, err := newUpstreamProxyMITMServerTLSConfig(authority, "api.example.com", time.Now().UTC())
	if err != nil {
		t.Fatalf("newUpstreamProxyMITMServerTLSConfig() error = %v", err)
	}
	if _, err := tlsConfig.GetConfigForClient(&tls.ClientHelloInfo{ServerName: "other.example.com"}); err == nil {
		t.Fatal("GetConfigForClient() error = nil, want SNI mismatch error")
	}
}

func TestUpstreamProxyHTTPRequestMustMatchConnectTarget(t *testing.T) {
	t.Parallel()

	for _, request := range []*http.Request{
		{Host: "other.example", URL: &url.URL{Path: "/"}},
		{Host: "api.example:444", URL: &url.URL{Path: "/"}},
		{Host: "api.example", URL: &url.URL{Scheme: "https", Host: "other.example", Path: "/"}},
		{Host: "api.example", URL: &url.URL{Scheme: "http", Host: "api.example", Path: "/"}},
	} {
		if upstreamProxyHTTPRequestMatchesTarget(request, "api.example") {
			t.Fatalf("upstreamProxyHTTPRequestMatchesTarget(host=%q, url=%q) = true, want false", request.Host, request.URL)
		}
	}
}

func TestLoadUpstreamProxyCertificateAuthorityRenewsRootWithStableIdentity(t *testing.T) {
	t.Parallel()

	files := writeTestUpstreamProxyCA(t, "renewal")
	issuedAfter := time.Now().UTC()
	first, err := loadOrGenerateUpstreamProxyCA(files.config())
	if err != nil {
		t.Fatalf("first loadOrGenerateUpstreamProxyCA() error = %v", err)
	}
	assertTestUpstreamProxyRootValidity(t, first.certificate, issuedAfter)

	second, err := loadOrGenerateUpstreamProxyCA(files.config())
	if err != nil {
		t.Fatalf("second loadOrGenerateUpstreamProxyCA() error = %v", err)
	}
	if bytes.Equal(first.certificate.Raw, second.certificate.Raw) {
		t.Fatal("renewed root certificate is byte-for-byte identical to the previous root")
	}
	if first.certificate.SerialNumber.Cmp(second.certificate.SerialNumber) == 0 {
		t.Fatal("renewed root certificate reused the previous serial number")
	}
	firstPublicKey, err := x509.MarshalPKIXPublicKey(first.certificate.PublicKey)
	if err != nil {
		t.Fatalf("marshal first root public key: %v", err)
	}
	secondPublicKey, err := x509.MarshalPKIXPublicKey(second.certificate.PublicKey)
	if err != nil {
		t.Fatalf("marshal renewed root public key: %v", err)
	}
	if !bytes.Equal(firstPublicKey, secondPublicKey) {
		t.Fatal("renewed root certificate did not reuse the stable public key")
	}
	if !bytes.Equal(first.certificate.RawSubject, second.certificate.RawSubject) {
		t.Fatal("renewed root certificate changed the subject")
	}
	if !bytes.Equal(first.certificate.SubjectKeyId, second.certificate.SubjectKeyId) {
		t.Fatal("renewed root certificate changed the subject key identifier")
	}
	if bytes.Equal(first.certificatePEM, second.certificatePEM) {
		t.Fatal("renewed in-memory root certificate reused the previous PEM")
	}

	leafNow := time.Now().UTC()
	leaf, err := second.certificateForHost("api.example.com", leafNow)
	if err != nil {
		t.Fatalf("renewed root certificateForHost() error = %v", err)
	}
	previousRoots := x509.NewCertPool()
	previousRoots.AddCert(first.certificate)
	// TLS server 会把本次启动的新根附在 leaf 后发送。把它放入 Intermediates，验证客户端即使
	// 收到新根，也仍能选择自己已信任的旧根直接校验同一稳定私钥签发的 leaf。
	presentedCertificates := x509.NewCertPool()
	presentedCertificates.AddCert(second.certificate)
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{
		Roots:         previousRoots,
		Intermediates: presentedCertificates,
		DNSName:       "api.example.com",
		CurrentTime:   leafNow,
	}); err != nil {
		t.Fatalf("verify leaf issued after renewal with previous root: %v", err)
	}
}

func TestLoadUpstreamProxyCertificateAuthorityIssuesTrustedLeaf(t *testing.T) {
	t.Parallel()

	files := writeTestUpstreamProxyCA(t, "stable")
	authority, err := loadOrGenerateUpstreamProxyCA(files.config())
	if err != nil {
		t.Fatalf("loadOrGenerateUpstreamProxyCA() error = %v", err)
	}
	leaf, err := authority.certificateForHost("api.example.com", time.Now().UTC())
	if err != nil {
		t.Fatalf("certificateForHost() error = %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(authority.certificate)
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "api.example.com"}); err != nil {
		t.Fatalf("verify issued leaf: %v", err)
	}
}

func TestUpstreamProxyCACertificateHandlerReturnsGeneratedCertificate(t *testing.T) {
	t.Parallel()

	files := writeTestUpstreamProxyCA(t, "handler")
	handler := NewHandler(files.config(), NewService(nil))
	authority, err := handler.loadUpstreamProxyCA()
	if err != nil {
		t.Fatalf("load generated handler CA: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/code/upstreamproxy/ca-cert", nil)
	recorder := httptest.NewRecorder()
	handler.handleUpstreamProxyCACertificate(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CA handler status = %d, want 200", response.StatusCode)
	}
	certificatePEM, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read CA response: %v", err)
	}
	block, rest := pem.Decode(certificatePEM)
	if block == nil || len(rest) != 0 {
		t.Fatalf("invalid CA response: block=%v rest=%q", block, rest)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse CA response: %v", err)
	}
	if !certificate.Equal(authority.certificate) {
		t.Fatal("CA handler did not return the in-memory startup certificate")
	}
}

func TestUpstreamProxyMITMDecryptsAndForwardsHTTPRequest(t *testing.T) {
	t.Parallel()

	files := writeTestUpstreamProxyCA(t, "tunnel")
	upstreamRequests := make(chan *http.Request, 1)
	dialTargets := make(chan string, 1)
	handler := NewHandler(files.config(), NewService(nil))
	authority, err := handler.loadUpstreamProxyCA()
	if err != nil {
		t.Fatalf("load generated tunnel CA: %v", err)
	}
	handler.upstreamProxy.dialTLS = func(_ context.Context, resolvedTarget string, serverName string) (net.Conn, error) {
		dialTargets <- resolvedTarget + "|" + serverName
		proxySide, targetSide := net.Pipe()
		go serveTestMITMUpstream(targetSide, upstreamRequests)
		return proxySide, nil
	}
	server := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(connection *websocket.Conn) {
			handler.serveUpstreamProxyTunnel(connection, "cse_test", "sk-ant-si-test")
		},
	})
	defer server.Close()

	connection := dialTestUpstreamProxyWebSocket(t, server.URL)
	defer connection.Close()
	authorization := base64.StdEncoding.EncodeToString([]byte("cse_test:sk-ant-si-test"))
	connectHead := "CONNECT 1.1.1.1:443 HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
	if err := websocket.Message.Send(connection, encodeUpstreamProxyChunk([]byte(connectHead))); err != nil {
		t.Fatalf("send CONNECT chunk: %v", err)
	}
	status, err := receiveUpstreamProxyChunk(connection)
	if err != nil {
		t.Fatalf("receive CONNECT status: %v", err)
	}
	if !strings.HasPrefix(string(status), "HTTP/1.1 200 Connection Established") {
		t.Fatalf("CONNECT status = %q", status)
	}
	if dialTarget := <-dialTargets; dialTarget != "1.1.1.1:443|1.1.1.1" {
		t.Fatalf("dial target = %q", dialTarget)
	}

	roots := x509.NewCertPool()
	roots.AddCert(authority.certificate)
	clientTLS := tls.Client(newUpstreamProxyChunkConn(connection), &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: "1.1.1.1",
		NextProtos: []string{"http/1.1"},
	})
	defer clientTLS.Close()
	if err := clientTLS.Handshake(); err != nil {
		t.Fatalf("MITM client TLS handshake: %v", err)
	}
	if got := clientTLS.ConnectionState().NegotiatedProtocol; got != "http/1.1" {
		t.Fatalf("negotiated protocol = %q, want http/1.1", got)
	}
	request, err := http.NewRequest(http.MethodGet, "https://1.1.1.1/robots.txt?source=mitm", nil)
	if err != nil {
		t.Fatalf("new HTTPS request: %v", err)
	}
	request.Close = true
	if err := request.Write(clientTLS); err != nil {
		t.Fatalf("write decrypted HTTP request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientTLS), request)
	if err != nil {
		t.Fatalf("read MITM response: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read MITM response body: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "mitm-ok" {
		t.Fatalf("MITM response = %d %q", response.StatusCode, body)
	}
	captured := <-upstreamRequests
	if captured.Host != "1.1.1.1" || captured.URL.RequestURI() != "/robots.txt?source=mitm" {
		t.Fatalf("unexpected decrypted upstream request: host=%q uri=%q", captured.Host, captured.URL.RequestURI())
	}
	if captured.Header.Get("Proxy-Authorization") != "" {
		t.Fatal("Proxy-Authorization leaked to upstream")
	}
}

type testUpstreamProxyCAFiles struct {
	keyFile string
}

func writeTestUpstreamProxyCA(t *testing.T, name string) testUpstreamProxyCAFiles {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test CA private key: %v", err)
	}
	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal test CA key: %v", err)
	}
	directory := t.TempDir()
	keyFile := filepath.Join(directory, name+"-ca-key.pem")
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKeyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write test CA key: %v", err)
	}
	return testUpstreamProxyCAFiles{keyFile: keyFile}
}

func (files testUpstreamProxyCAFiles) config() config.Config {
	return config.Config{
		CodeSessionUpstreamProxyMITMEnabled: true,
		CodeSessionUpstreamProxyCAKeyFile:   files.keyFile,
	}
}

func assertTestUpstreamProxyRootValidity(t *testing.T, certificate *x509.Certificate, issuedAfter time.Time) {
	t.Helper()
	if certificate.NotBefore.Before(issuedAfter.Add(-6*time.Minute)) || certificate.NotBefore.After(time.Now().UTC()) {
		t.Fatalf("generated CA NotBefore = %s, want approximately five minutes before startup", certificate.NotBefore)
	}
	if got := certificate.NotAfter.Sub(certificate.NotBefore.Add(5 * time.Minute)); got != 365*24*time.Hour {
		t.Fatalf("generated CA validity from startup = %s, want %s", got, 365*24*time.Hour)
	}
}

func dialTestUpstreamProxyWebSocket(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(serverURL, "http"), "http://localhost")
	if err != nil {
		t.Fatalf("new websocket config: %v", err)
	}
	connection, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return connection
}

func serveTestMITMUpstream(connection net.Conn, requests chan<- *http.Request) {
	defer connection.Close()
	request, err := http.ReadRequest(bufio.NewReader(connection))
	if err != nil {
		return
	}
	requests <- request
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"text/plain"}},
		Body:          io.NopCloser(strings.NewReader("mitm-ok")),
		ContentLength: int64(len("mitm-ok")),
		Close:         true,
	}
	_ = response.Write(connection)
}
