package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

type appErrorResponse struct {
	Type      string                     `json:"type"`
	RequestID string                     `json:"request_id"`
	Error     map[string]json.RawMessage `json:"error"`
}

func TestErrorAdapterRejectsMalformedAndUnknownErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "ordinary error", err: errors.New("database password=secret")},
		{name: "unknown kind", err: apperr.New(99, "Do not expose", nil)},
		{name: "empty message", err: apperr.New(apperr.NotFound, "", nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, logs := testErrorAdapter()
			recorder := httptest.NewRecorder()
			request := testErrorRequest()

			adapter.Write(recorder, request, tt.err)

			response := decodeAppErrorResponse(t, recorder)
			assertAppError(t, recorder, response, http.StatusInternalServerError, "api_error", "Internal server error")
			if count := strings.Count(logs.String(), "\n"); count != 1 {
				t.Fatalf("error log count = %d, want 1: %s", count, logs.String())
			}
			if strings.Contains(logs.String(), "token=secret") {
				t.Fatalf("error log leaked query string: %s", logs.String())
			}
		})
	}
}

func TestErrorAdapterKeepsInternalCauseOutOfResponse(t *testing.T) {
	adapter, logs := testErrorAdapter()
	recorder := httptest.NewRecorder()
	request := testErrorRequest()
	err := apperr.New(
		apperr.Internal,
		"Could not update vault",
		errors.New("database password=secret"),
	)

	adapter.Write(recorder, request, err)

	response := decodeAppErrorResponse(t, recorder)
	assertAppError(t, recorder, response, http.StatusInternalServerError, "api_error", "Could not update vault")
	if strings.Contains(recorder.Body.String(), "database password") {
		t.Fatalf("response leaked internal cause: %s", recorder.Body.String())
	}
	for _, field := range []string{
		`"request_id":"req_test"`,
		`"method":"GET"`,
		`"path":"/v1/vaults"`,
		`"error_kind":"internal"`,
		`"error":"database password=secret"`,
	} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("error log missing %s: %s", field, logs.String())
		}
	}
	if count := strings.Count(logs.String(), "\n"); count != 1 {
		t.Fatalf("error log count = %d, want 1: %s", count, logs.String())
	}
}

func TestErrorAdapterMapsApplicationErrors(t *testing.T) {
	tests := []struct {
		name         string
		kind         apperr.Kind
		status       int
		errorType    string
		publicText   string
		wantErrorLog bool
	}{
		{name: "invalid argument", kind: apperr.InvalidArgument, status: http.StatusBadRequest, errorType: "invalid_request_error", publicText: "Invalid request"},
		{name: "invalid state", kind: apperr.InvalidState, status: http.StatusConflict, errorType: "invalid_request_error", publicText: "Resource is not ready"},
		{name: "precondition failed", kind: apperr.PreconditionFailed, status: http.StatusPreconditionFailed, errorType: "invalid_request_error", publicText: "Precondition failed"},
		{name: "request too large", kind: apperr.RequestTooLarge, status: http.StatusRequestEntityTooLarge, errorType: "invalid_request_error", publicText: "Request body exceeds maximum size"},
		{name: "unauthenticated", kind: apperr.Unauthenticated, status: http.StatusUnauthorized, errorType: "authentication_error", publicText: "Missing API key"},
		{name: "billing", kind: apperr.Billing, status: http.StatusPaymentRequired, errorType: "billing_error", publicText: "Billing issue"},
		{name: "permission denied", kind: apperr.PermissionDenied, status: http.StatusForbidden, errorType: "permission_error", publicText: "Permission denied"},
		{name: "not found", kind: apperr.NotFound, status: http.StatusNotFound, errorType: "not_found_error", publicText: "Not found"},
		{name: "conflict", kind: apperr.Conflict, status: http.StatusConflict, errorType: "conflict_error", publicText: "Already exists"},
		{name: "rate limited", kind: apperr.RateLimited, status: http.StatusTooManyRequests, errorType: "rate_limit_error", publicText: "Rate limited"},
		{name: "timeout", kind: apperr.Timeout, status: http.StatusGatewayTimeout, errorType: "timeout_error", publicText: "Request timed out", wantErrorLog: true},
		{name: "internal", kind: apperr.Internal, status: http.StatusInternalServerError, errorType: "api_error", publicText: "Internal error", wantErrorLog: true},
		{name: "unavailable", kind: apperr.Unavailable, status: http.StatusServiceUnavailable, errorType: "api_error", publicText: "Temporarily unavailable", wantErrorLog: true},
		{name: "overloaded", kind: apperr.Overloaded, status: 529, errorType: "overloaded_error", publicText: "Overloaded", wantErrorLog: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, logs := testErrorAdapter()
			recorder := httptest.NewRecorder()
			request := testErrorRequest()
			err := apperr.New(tt.kind, tt.publicText, errors.New("private cause"))

			adapter.Write(recorder, request, err)

			response := decodeAppErrorResponse(t, recorder)
			assertAppError(t, recorder, response, tt.status, tt.errorType, tt.publicText)
			if got := logs.Len() != 0; got != tt.wantErrorLog {
				t.Fatalf("Error log present = %v, want %v: %s", got, tt.wantErrorLog, logs.String())
			}
			if _, ok := response.Error["code"]; ok {
				t.Fatalf("response unexpectedly exposed error.code: %s", recorder.Body.String())
			}
		})
	}
}

func TestErrorAdapterWritesWrappedClientErrorWithoutErrorLog(t *testing.T) {
	adapter, logs := testErrorAdapter()
	recorder := httptest.NewRecorder()
	request := testErrorRequest()
	err := fmt.Errorf("retrieve vault: %w", apperr.New(
		apperr.NotFound,
		"Vault not found: vlt_missing",
		errors.New("row missing"),
	))

	adapter.Wrap(func(http.ResponseWriter, *http.Request) error { return err })(recorder, request)

	response := decodeAppErrorResponse(t, recorder)
	assertAppError(t, recorder, response, http.StatusNotFound, "not_found_error", "Vault not found: vlt_missing")
	if logs.Len() != 0 {
		t.Fatalf("client error produced Error log: %s", logs.String())
	}
	if _, ok := response.Error["code"]; ok {
		t.Fatalf("response unexpectedly exposed error.code: %s", recorder.Body.String())
	}
}

func testErrorAdapter() (*ErrorAdapter, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return NewErrorAdapter(logger), logs
}

func testErrorRequest() *http.Request {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/v1/vaults?token=secret", nil)
	return request.WithContext(WithRequestID(request.Context(), "req_test"))
}

func decodeAppErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder) appErrorResponse {
	t.Helper()
	var response appErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v: %s", err, recorder.Body.String())
	}
	return response
}

func assertAppError(t *testing.T, recorder *httptest.ResponseRecorder, response appErrorResponse, status int, errorType, message string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
	if response.Type != "error" || response.RequestID != "req_test" {
		t.Fatalf("response envelope = %+v", response)
	}
	var gotType, gotMessage string
	if err := json.Unmarshal(response.Error["type"], &gotType); err != nil {
		t.Fatalf("decode error.type: %v", err)
	}
	if err := json.Unmarshal(response.Error["message"], &gotMessage); err != nil {
		t.Fatalf("decode error.message: %v", err)
	}
	if gotType != errorType || gotMessage != message {
		t.Fatalf("error = (%q, %q), want (%q, %q)", gotType, gotMessage, errorType, message)
	}
}

func TestErrorAdapterHonorsCustomWireErrorType(t *testing.T) {
	adapter, logs := testErrorAdapter()
	recorder := httptest.NewRecorder()
	request := testErrorRequest()
	err := apperr.NewWithType(apperr.Conflict, "memory_store_limit_error", "at most 8 memory stores are supported per session", nil)

	adapter.Write(recorder, request, err)

	response := decodeAppErrorResponse(t, recorder)
	assertAppError(t, recorder, response, http.StatusConflict, "memory_store_limit_error", "at most 8 memory stores are supported per session")
	if logs.Len() != 0 {
		t.Fatalf("4xx error must not be logged, got: %s", logs.String())
	}
}
