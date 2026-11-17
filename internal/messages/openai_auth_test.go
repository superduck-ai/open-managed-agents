package messages

import (
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIRejectsNonAPICredentials(t *testing.T) {
	for _, backend := range []string{"chat_completions", "responses"} {
		for _, credential := range []string{auth.CredentialTypePlatformSession, auth.CredentialTypeEnvironmentKey, auth.CredentialTypeWorkToken, ""} {
			for _, session := range []string{"", "cse_untrusted"} {
				request := httptest.NewRequest(http.MethodPost, "/v1/"+backend, nil)
				request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{CredentialType: credential, CodeSessionExternalID: session}))
				recorder := httptest.NewRecorder()
				(&Handler{}).openAI(recorder, request, backend)
				if recorder.Code != http.StatusForbidden {
					t.Fatalf("%s/%s/%s：%d", backend, credential, session, recorder.Code)
				}
			}
		}
	}
}
