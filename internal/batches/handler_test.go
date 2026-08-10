package batches

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestCreateRejectsTrailingJSON(t *testing.T) {
	handler := newRequestBodyTestHandler(1024)
	request := newCreateRequest(`{"requests":[{"custom_id":"req_1","params":{}}]} {}`)
	recorder := httptest.NewRecorder()

	handler.create(recorder, request, false, nil)

	assertCreateBodyError(t, recorder, http.StatusBadRequest, "Invalid JSON body")
}

func TestCreatePreservesBodyTooLargeResponse(t *testing.T) {
	handler := newRequestBodyTestHandler(1)
	request := newCreateRequest(`{"requests":[]}`)
	recorder := httptest.NewRecorder()

	handler.create(recorder, request, false, nil)

	assertCreateBodyError(t, recorder, http.StatusRequestEntityTooLarge, "Request body exceeds maximum size")
}

func newRequestBodyTestHandler(maxBodyBytes int64) *Handler {
	return &Handler{cfg: config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{APIKey: "test-key"},
		Batch:             config.BatchConfig{MaxBodyBytes: maxBodyBytes},
	}}
}

func newCreateRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/messages/batches", strings.NewReader(body))
	principal := auth.Principal{
		CredentialType:   auth.CredentialTypeAPIKey,
		APIKeyExternalID: "key_test",
		WorkspaceUUID:    "workspace",
	}
	return request.WithContext(auth.WithPrincipal(request.Context(), principal))
}

func assertCreateBodyError(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantMessage string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d", recorder.Code, wantStatus)
	}
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Message != wantMessage {
		t.Fatalf("message = %q, want %q", response.Error.Message, wantMessage)
	}
}
