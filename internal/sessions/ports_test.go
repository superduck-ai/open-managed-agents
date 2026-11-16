package sessions

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

func sessionPortRequest(t *testing.T, credentialType, port string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/sesn_1/ports/"+port, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("session_id", "sesn_1")
	routeContext.URLParams.Add("port", port)
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = auth.WithPrincipal(ctx, auth.Principal{CredentialType: credentialType, WorkspaceUUID: "ws"})
	return request.WithContext(ctx)
}

func TestRetrievePortRouteRejectsBeforeLoadingSession(t *testing.T) {
	codec, err := sessionports.NewLabelCodec(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	enabled := sessionports.NewWithCodec(config.SessionPortsConfig{DomainSuffix: "preview.example.com", URLScheme: "https"}, 3001, codec)
	for name, testCase := range map[string]struct {
		settings   *sessionports.Settings
		credential string
		port       string
		kind       apperr.Kind
	}{
		"disabled":          {&sessionports.Settings{}, auth.CredentialTypeAPIKey, "4000", apperr.NotFound},
		"work token":        {enabled, auth.CredentialTypeWorkToken, "4000", apperr.PermissionDenied},
		"non numeric":       {enabled, auth.CredentialTypeAPIKey, "abc", apperr.InvalidArgument},
		"out of range":      {enabled, auth.CredentialTypeAPIKey, "65536", apperr.InvalidArgument},
		"envd port":         {enabled, auth.CredentialTypeAPIKey, "49983", apperr.InvalidArgument},
		"sandbox mcp port":  {enabled, auth.CredentialTypeAPIKey, "3001", apperr.InvalidArgument},
		"zero control port": {enabled, auth.CredentialTypeAPIKey, "0", apperr.InvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			handler := (&Handler{}).WithSessionPorts(testCase.settings)
			err := handler.retrievePortRoute(httptest.NewRecorder(), sessionPortRequest(t, testCase.credential, testCase.port))
			var appErr *apperr.Error
			if !errors.As(err, &appErr) || appErr.Kind != testCase.kind {
				t.Fatalf("error = %v, want kind %v", err, testCase.kind)
			}
		})
	}
}
