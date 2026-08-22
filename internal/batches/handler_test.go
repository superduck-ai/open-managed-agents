package batches

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestBatchConfiguredModelErrorsPreserveServerFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind apperr.Kind
	}{
		{name: "database failure", err: errors.New("database unavailable"), kind: apperr.Internal},
		{name: "provider missing", err: llmproviders.ErrNotConfigured, kind: apperr.Unavailable},
		{name: "model invalid", err: newModelValidationError(errors.New("model is not configured")), kind: apperr.InvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped, ok := errors.AsType[*apperr.Error](configuredModelError(test.err))
			if !ok || mapped.Kind != test.kind {
				t.Fatalf("configuredModelError() = %#v, want kind %v", mapped, test.kind)
			}
		})
	}
}

func TestBatchRequestModelRejectsSurroundingWhitespace(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "leading whitespace", body: `{"model":" model-a"}`, wantErr: true},
		{name: "trailing whitespace", body: `{"model":"model-a "}`, wantErr: true},
		{name: "missing model", body: `{}`, wantErr: true},
		{name: "valid model unchanged", body: `{"model":"model-a"}`, want: "model-a"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, err := batchRequestModel(json.RawMessage(test.body))
			if (err != nil) != test.wantErr || model != test.want {
				t.Fatalf("batchRequestModel() = %q, %v", model, err)
			}
		})
	}
}

func TestCreatePreservesBodyTooLargeResponse(t *testing.T) {
	handler := newRequestBodyTestHandler(1)
	request := newCreateRequest(`{"requests":[]}`)
	recorder := httptest.NewRecorder()

	err := handler.create(recorder, request, false, nil)
	httpapi.NewErrorAdapter(nil).Write(recorder, request, err)

	assertCreateBodyError(t, recorder, http.StatusRequestEntityTooLarge, "Request body exceeds maximum size")
}

func newRequestBodyTestHandler(maxBodyBytes int64) *Handler {
	return &Handler{cfg: config.Config{
		Batch: config.BatchConfig{MaxBodyBytes: maxBodyBytes},
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
