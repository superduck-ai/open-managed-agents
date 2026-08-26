package tunnels

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestManagementSuccessLogContainsOnlySafeIdentifiers(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	request := httptest.NewRequest(http.MethodPost, "/v1/tunnels/tunnel_0123456789abcdef0123456789abcdef/rotate_token", strings.NewReader(`{"reason":"private reason"}`))
	request.Header.Set("Authorization", "Bearer secret-token")
	request.Header.Set("Cookie", "sessionKey=secret-session")
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		CredentialType: auth.CredentialTypePlatformSession,
		UserUUID:       "user-safe-id",
	})
	ctx = httpapi.WithRequestID(ctx, "request-safe-id")
	request = request.WithContext(ctx)

	logScopedManagementSuccess(
		logger, request, "mcp tunnel token rotated", "tunnel_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "org-safe-id", "workspace-safe-id",
	)
	logged := output.String()
	for _, expected := range []string{"request-safe-id", "org-safe-id", "workspace-safe-id", "tunnel_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "user-safe-id"} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("management log %q does not contain %q", logged, expected)
		}
	}
	for _, secret := range []string{"secret-token", "secret-session", "private reason"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("management log contains secret %q: %s", secret, logged)
		}
	}
}
