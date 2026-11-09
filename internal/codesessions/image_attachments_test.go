package codesessions

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestImageAttachmentRoutesRejectMissingCredentialsAndEpochMismatch(t *testing.T) {
	handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil).WithImageAttachments(nil)
	identity := testSessionCredentialIdentity()
	identity.WorkerEpoch = 2
	token, err := handler.service.credentials.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	handler.RegisterV1Routes(router)
	for _, scenario := range []struct {
		token, epoch string
		status       int
	}{
		{"", "2", http.StatusUnauthorized},
		{token, "", http.StatusConflict},
		{token, "1", http.StatusConflict},
	} {
		request := httptest.NewRequest(http.MethodHead, "/code/sessions/"+identity.SessionID+"/worker/image-attachments/invalid", nil)
		if scenario.token != "" {
			request.Header.Set("Authorization", "Bearer "+scenario.token)
		}
		request.Header.Set("X-Worker-Epoch", scenario.epoch)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != scenario.status {
			t.Fatalf("epoch=%q status=%d body=%s", scenario.epoch, response.Code, response.Body.String())
		}
	}
}
