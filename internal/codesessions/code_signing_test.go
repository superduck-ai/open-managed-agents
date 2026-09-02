package codesessions

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestGitCommitSigningRejectsUnconfiguredCredentials(t *testing.T) {
	if _, err := (*SessionCredentials)(nil).signGitCommit([]byte("commit")); err == nil {
		t.Fatal("signGitCommit() accepted nil credentials")
	}
	if _, err := (*SessionCredentials)(nil).GitSigningPublicKey(); err == nil {
		t.Fatal("GitSigningPublicKey() accepted nil credentials")
	}
	if _, err := (*Service)(nil).SignGitCommit([]byte("commit")); err == nil {
		t.Fatal("SignGitCommit() accepted nil service")
	}
}

func TestGitCommitSigningProducesVerifiableSSHSIG(t *testing.T) {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	credentials := newTestSessionCredentials(t, &now)
	contents := []byte("tree 0123456789abcdef\nauthor Example <example@example.com> 0 +0000\n\nmessage\n")

	signature, err := credentials.signGitCommit(contents)
	if err != nil {
		t.Fatalf("signGitCommit() error = %v", err)
	}
	second, err := credentials.signGitCommit(contents)
	if err != nil {
		t.Fatalf("second signGitCommit() error = %v", err)
	}
	if signature != second {
		t.Fatal("Ed25519 signature is not deterministic for identical contents")
	}
	parsed := parseTestSSHSIG(t, signature)
	if parsed.namespace != sshSignatureNamespace || parsed.hashAlgorithm != sshSignatureHash || len(parsed.reserved) != 0 {
		t.Fatalf("unexpected SSHSIG metadata: namespace=%q reserved=%x hash=%q", parsed.namespace, parsed.reserved, parsed.hashAlgorithm)
	}
	if !bytes.Equal(parsed.publicKey, credentials.publicKey) {
		t.Fatal("SSHSIG public key does not match SessionCredentials")
	}
	digest := sha512.Sum512(contents)
	signedData := append([]byte(nil), sshSignatureMagic...)
	signedData = appendSSHString(signedData, []byte(parsed.namespace))
	signedData = appendSSHString(signedData, parsed.reserved)
	signedData = appendSSHString(signedData, []byte(parsed.hashAlgorithm))
	signedData = appendSSHString(signedData, digest[:])
	if !ed25519.Verify(parsed.publicKey, signedData, parsed.signature) {
		t.Fatal("SSHSIG Ed25519 signature did not verify")
	}
	changedDigest := sha512.Sum512(append(append([]byte(nil), contents...), '!'))
	changedData := append([]byte(nil), sshSignatureMagic...)
	changedData = appendSSHString(changedData, []byte(parsed.namespace))
	changedData = appendSSHString(changedData, parsed.reserved)
	changedData = appendSSHString(changedData, []byte(parsed.hashAlgorithm))
	changedData = appendSSHString(changedData, changedDigest[:])
	if ed25519.Verify(parsed.publicKey, changedData, parsed.signature) {
		t.Fatal("SSHSIG verified different contents")
	}

	publicKey, err := credentials.GitSigningPublicKey()
	if err != nil {
		t.Fatalf("GitSigningPublicKey() error = %v", err)
	}
	parts := strings.Fields(publicKey)
	if len(parts) != 3 || parts[0] != sshEd25519Algorithm || parts[2] != "oma-code-signing" {
		t.Fatalf("unexpected OpenSSH public key line: %q", publicKey)
	}
	publicBlob, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode OpenSSH public key: %v", err)
	}
	if !bytes.Equal(publicBlob, parsed.publicKeyBlob) {
		t.Fatal("exported public key does not match SSHSIG public key blob")
	}
}

func TestGitCommitSigningInteroperatesWithOpenSSH(t *testing.T) {
	sshKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	credentials := newTestSessionCredentials(t, &now)
	contents := []byte("commit payload for OpenSSH\n")
	signature, err := credentials.signGitCommit(contents)
	if err != nil {
		t.Fatalf("signGitCommit() error = %v", err)
	}
	signatureFile := filepath.Join(t.TempDir(), "commit.sig")
	if err := os.WriteFile(signatureFile, []byte(signature), 0o600); err != nil {
		t.Fatalf("write signature file: %v", err)
	}
	command := exec.Command(sshKeygen, "-Y", "check-novalidate", "-n", sshSignatureNamespace, "-s", signatureFile)
	command.Stdin = bytes.NewReader(contents)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen rejected SSHSIG: %v: %s", err, output)
	}
	command = exec.Command(sshKeygen, "-Y", "check-novalidate", "-n", sshSignatureNamespace, "-s", signatureFile)
	command.Stdin = strings.NewReader("different payload\n")
	if err := command.Run(); err == nil {
		t.Fatal("ssh-keygen accepted SSHSIG for different contents")
	}
}

func TestSignCommitHTTPContract(t *testing.T) {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	credentials := newTestSessionCredentials(t, &now)
	service := NewServiceWithCredentials(nil, credentials, nil)
	handler := NewHandler(configForCodeSigningTest(), service, nil, nil)
	router := chi.NewRouter()
	handler.RegisterV1Routes(router)

	managedIdentity := testSessionCredentialIdentity()
	managedIdentity.WorkerEpoch = 1
	managedToken, err := credentials.Issue(managedIdentity)
	if err != nil {
		t.Fatalf("issue managed token: %v", err)
	}
	legacyIdentity := testSessionCredentialIdentity()
	legacyToken, err := credentials.Issue(legacyIdentity)
	if err != nil {
		t.Fatalf("issue legacy token: %v", err)
	}

	failures := []struct {
		name       string
		sessionID  string
		token      string
		body       string
		wantStatus int
	}{
		{name: "missing token", sessionID: "cse_test", body: `{"contents":"commit"}`, wantStatus: http.StatusUnauthorized},
		{name: "legacy token", sessionID: "cse_test", token: legacyToken, body: `{"contents":"commit"}`, wantStatus: http.StatusUnauthorized},
		{name: "mismatched session", sessionID: "cse_other", token: managedToken, body: `{"contents":"commit"}`, wantStatus: http.StatusUnauthorized},
		{name: "malformed JSON", sessionID: "cse_test", token: managedToken, body: `{"contents":`, wantStatus: http.StatusBadRequest},
		{name: "null body", sessionID: "cse_test", token: managedToken, body: `null`, wantStatus: http.StatusBadRequest},
		{name: "empty contents", sessionID: "cse_test", token: managedToken, body: `{"contents":""}`, wantStatus: http.StatusBadRequest},
		{name: "invalid object format", sessionID: "cse_test", token: managedToken, body: `{"contents":"commit","git_object_format":"md5"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid source", sessionID: "cse_test", token: managedToken, body: `{"contents":"commit","source":{"type":"url"}}`, wantStatus: http.StatusBadRequest},
	}
	for _, test := range failures {
		t.Run("failure "+test.name, func(t *testing.T) {
			response := performSignCommitRequest(t, router, test.sessionID, test.token, test.body)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}

	t.Run("failure oversized body", func(t *testing.T) {
		body := `{"contents":"` + strings.Repeat("a", maxIngressBodySize) + `"}`
		response := performSignCommitRequest(t, router, "cse_test", managedToken, body)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
		}
	})

	t.Run("success", func(t *testing.T) {
		contents := "  exact commit contents\n"
		body, err := json.Marshal(signCommitRequest{
			Contents: contents,
			Source: &signCommitSource{
				Type: "git_repository",
				GitInfo: &signCommitGitInfo{
					Type: "github",
					Repo: "superduck-ai/open-managed-agents",
					Ref:  "refs/heads/main",
				},
			},
			GitObjectFormat: "sha256",
		})
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		response := performSignCommitRequest(t, router, "cse_test", managedToken, string(body))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var decoded signCommitResponse
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		parsed := parseTestSSHSIG(t, decoded.Signature)
		digest := sha512.Sum512([]byte(contents))
		signedData := append([]byte(nil), sshSignatureMagic...)
		signedData = appendSSHString(signedData, []byte(parsed.namespace))
		signedData = appendSSHString(signedData, parsed.reserved)
		signedData = appendSSHString(signedData, []byte(parsed.hashAlgorithm))
		signedData = appendSSHString(signedData, digest[:])
		if !ed25519.Verify(parsed.publicKey, signedData, parsed.signature) {
			t.Fatal("HTTP response did not sign the exact contents")
		}
	})
}

func configForCodeSigningTest() config.Config {
	return config.Config{}
}

func performSignCommitRequest(t *testing.T, handler http.Handler, sessionID, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/code/sessions/"+sessionID+"/sign-commit", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type testSSHSIG struct {
	publicKeyBlob []byte
	publicKey     ed25519.PublicKey
	namespace     string
	reserved      []byte
	hashAlgorithm string
	signature     []byte
}

func parseTestSSHSIG(t *testing.T, armored string) testSSHSIG {
	t.Helper()
	if !strings.HasSuffix(armored, "\n") {
		t.Fatal("armored signature is missing trailing newline")
	}
	lines := strings.Split(strings.TrimSuffix(armored, "\n"), "\n")
	if len(lines) < 3 || lines[0] != "-----BEGIN SSH SIGNATURE-----" || lines[len(lines)-1] != "-----END SSH SIGNATURE-----" {
		t.Fatalf("invalid SSHSIG armor: %q", armored)
	}
	for _, line := range lines[1 : len(lines)-1] {
		if len(line) > sshArmorLineLength {
			t.Fatalf("armored line length = %d, want <= %d", len(line), sshArmorLineLength)
		}
	}
	blob, err := base64.StdEncoding.DecodeString(strings.Join(lines[1:len(lines)-1], ""))
	if err != nil {
		t.Fatalf("decode SSHSIG armor: %v", err)
	}
	reader := bytes.NewReader(blob)
	magic := make([]byte, len(sshSignatureMagic))
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != sshSignatureMagic {
		t.Fatalf("read SSHSIG magic = %q, %v", magic, err)
	}
	var version uint32
	if err := binary.Read(reader, binary.BigEndian, &version); err != nil || version != 1 {
		t.Fatalf("read SSHSIG version = %d, %v", version, err)
	}
	publicKeyBlob := readTestSSHString(t, reader)
	namespace := string(readTestSSHString(t, reader))
	reserved := readTestSSHString(t, reader)
	hashAlgorithm := string(readTestSSHString(t, reader))
	signatureBlob := bytes.NewReader(readTestSSHString(t, reader))
	if reader.Len() != 0 {
		t.Fatalf("SSHSIG has %d trailing bytes", reader.Len())
	}
	algorithm := string(readTestSSHString(t, signatureBlob))
	signature := readTestSSHString(t, signatureBlob)
	if algorithm != sshEd25519Algorithm || signatureBlob.Len() != 0 {
		t.Fatalf("unexpected signature blob algorithm=%q trailing=%d", algorithm, signatureBlob.Len())
	}
	publicReader := bytes.NewReader(publicKeyBlob)
	publicAlgorithm := string(readTestSSHString(t, publicReader))
	publicKey := readTestSSHString(t, publicReader)
	if publicAlgorithm != sshEd25519Algorithm || len(publicKey) != ed25519.PublicKeySize || publicReader.Len() != 0 {
		t.Fatalf("unexpected public key blob algorithm=%q key_bytes=%d trailing=%d", publicAlgorithm, len(publicKey), publicReader.Len())
	}
	return testSSHSIG{
		publicKeyBlob: publicKeyBlob,
		publicKey:     ed25519.PublicKey(publicKey),
		namespace:     namespace,
		reserved:      reserved,
		hashAlgorithm: hashAlgorithm,
		signature:     signature,
	}
}

func readTestSSHString(t *testing.T, reader *bytes.Reader) []byte {
	t.Helper()
	var length uint32
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		t.Fatalf("read SSH string length: %v", err)
	}
	if uint64(length) > uint64(reader.Len()) {
		t.Fatalf("SSH string length = %d, remaining = %d", length, reader.Len())
	}
	value := make([]byte, length)
	if _, err := io.ReadFull(reader, value); err != nil {
		t.Fatalf("read SSH string: %v", err)
	}
	return value
}
