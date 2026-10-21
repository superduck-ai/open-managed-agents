package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

var filestoreAuthPaths = []string{
	"/v1/filestore/fs/listDirectory",
	"/v1/filestore/fs/makeDirectory",
	"/v1/filestore/fs/removeDirectory",
	"/v1/filestore/fs/createFile",
	"/v1/filestore/fs/copyFile",
	"/v1/filestore/fs/moveFile",
	"/v1/filestore/fs/moveDirectory",
	"/v1/filestore/fs/readFile",
	"/v1/filestore/fs/removeFile",
	"/v1/filestore/fs/readMetadata",
}

type filestoreAuthCredentials struct {
	ingress   *codesessions.SessionCredentials
	filestore *filestore.TokenCredentials
}

func TestFilestoreRoutingUsesResourceBoundary(t *testing.T) {
	t.Parallel()

	credentials := newFilestoreAuthCredentials(t)
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials.ingress,
		FilestoreCredentials:   credentials.filestore,
	})
	for _, requestPath := range append([]string{
		"/v1/filestore",
		"/v1/filestore/",
		"/v1/filestore/fs",
		"/v1/filestore/fs/unknownOperation",
	}, filestoreAuthPaths...) {
		requestPath := requestPath
		t.Run("filestore_"+strings.NewReplacer("/", "_", " ", "space").Replace(requestPath), func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, requestPath, nil))
			assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
		})
	}

	for _, requestPath := range []string{
		"/v1/filestores",
		"/v1/filestores/fs/listDirectory",
		"/v1/filestore-backup",
		"/v1/filestor",
		"/v1/messages",
	} {
		requestPath := requestPath
		t.Run("ordinary_v1_"+strings.NewReplacer("/", "_", " ", "space").Replace(requestPath), func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, requestPath, nil))
			if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"type":"authentication_error"`) {
				t.Fatalf("response = %d %s, want ordinary v1 authentication envelope", response.Code, response.Body.String())
			}
		})
	}
}

func TestFilestoreProtocolOperationsAreRegistered(t *testing.T) {
	t.Parallel()

	handler := filestore.NewHandler(config.Config{}, filestore.NewService(config.Config{}, nil, nil, nil), nil)
	for _, requestPath := range filestoreAuthPaths {
		requestPath := requestPath
		t.Run(strings.TrimPrefix(requestPath, "/v1/filestore/fs/"), func(t *testing.T) {
			t.Parallel()
			handlerPath := strings.TrimPrefix(requestPath, "/v1/filestore/fs")
			request := httptest.NewRequest(http.MethodPost, handlerPath, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("POST %s status = %d, want registered handler authentication response %d: %s", requestPath, response.Code, http.StatusUnauthorized, response.Body.String())
			}
		})
	}
}

func TestFilestoreAuthMiddlewareWritesFlatErrors(t *testing.T) {
	t.Parallel()

	credentials := newFilestoreAuthCredentials(t)
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials.ingress,
		FilestoreCredentials:   credentials.filestore,
	})
	for _, requestPath := range []string{
		"/v1/filestore",
		"/v1/filestore/unknown",
		filestoreAuthPaths[0],
	} {
		requestPath := requestPath
		t.Run(strings.TrimPrefix(requestPath, "/v1/filestore"), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, requestPath, nil)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
		})
	}
}

func TestAuthenticateFilestoreOwnsProtocolError(t *testing.T) {
	t.Parallel()

	server := &Server{}
	for _, test := range []struct {
		name        string
		request     *http.Request
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "missing bearer token",
			request:     httptest.NewRequest(http.MethodPost, filestoreAuthPaths[0], nil),
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "unauthenticated",
			wantMessage: "Invalid bearer token",
		},
		{
			name: "authentication dependency unavailable",
			request: newFilestoreBearerRequest(
				http.MethodPost,
				filestoreAuthPaths[0],
				"not-a-jwt",
			),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "internal",
			wantMessage: "Authentication failed",
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, authErr := server.authenticateFilestore(test.request)
			if authErr == nil {
				t.Fatal("authenticateFilestore() error = nil")
			}
			if authErr.status != test.wantStatus ||
				authErr.code != test.wantCode ||
				authErr.message != test.wantMessage {
				t.Fatalf(
					"authenticateFilestore() error = %#v, want status=%d code=%q message=%q",
					authErr,
					test.wantStatus,
					test.wantCode,
					test.wantMessage,
				)
			}
		})
	}
}

func newFilestoreAuthCredentials(t *testing.T) filestoreAuthCredentials {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate filestore auth signing key: %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal filestore auth signing key: %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "code-session-ed25519.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if err := os.WriteFile(keyFile, data, 0o600); err != nil {
		t.Fatalf("write filestore auth signing key: %v", err)
	}
	credentialConfig := config.Config{
		Env: config.EnvironmentProd,
		CodeSession: config.CodeSessionConfig{
			JWTSigningPrivateKeyFile: keyFile,
		},
	}
	ingressCredentials, err := codesessions.NewSessionCredentials(credentialConfig)
	if err != nil {
		t.Fatalf("create session ingress credentials: %v", err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(credentialConfig)
	if err != nil {
		t.Fatalf("create filestore auth credentials: %v", err)
	}
	return filestoreAuthCredentials{ingress: ingressCredentials, filestore: filestoreCredentials}
}

func newFilestoreBearerRequest(method, requestPath, token string) *http.Request {
	request := httptest.NewRequest(method, requestPath, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func serveFilestoreAuthRequest(server http.Handler, method, requestPath, token string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.ServeHTTP(response, newFilestoreBearerRequest(method, requestPath, token))
	return response
}

func assertFlatFilestoreAuthError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()

	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode filestore error: %v: %s", err, response.Body.String())
	}
	if len(payload) != 2 || payload["code"] != code || payload["message"] != message {
		t.Fatalf("filestore error = %#v, want flat code=%q message=%q", payload, code, message)
	}
	if _, exists := payload["error"]; exists {
		t.Fatalf("filestore error unexpectedly contains nested error: %#v", payload)
	}
	if _, exists := payload["type"]; exists {
		t.Fatalf("filestore error unexpectedly contains type envelope: %#v", payload)
	}
}

// assertFilestoreHandlerReachedAfterAuth 断言请求已越过鉴权中间件，到达真实 Filestore Handler。
// 空请求体在不同协议端点上的校验文案不同，因此只锁定状态码与错误码，不比对 message。
