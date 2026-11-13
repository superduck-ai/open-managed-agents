package codesessions

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestMapCodeSessionLoadError(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "unexpected failure", cause: errors.New("database failed"), kind: apperr.Internal, message: "Could not load code session"},
		{name: "not found", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Code session not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapCodeSessionLoadError(test.cause, "code_session_test")
			appErr, ok := errors.AsType[*apperr.Error](mapped)
			if !ok {
				t.Fatalf("error type = %T, want *apperr.Error", mapped)
			}
			if appErr.Kind != test.kind || appErr.PublicMessage != test.message {
				t.Fatalf("error = (%v, %q), want (%v, %q)", appErr.Kind, appErr.PublicMessage, test.kind, test.message)
			}
			if !errors.Is(mapped, test.cause) {
				t.Fatalf("errors.Is(%v, cause) = false", mapped)
			}
		})
	}
}

func TestSessionContextFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		build   func(error) error
		message string
	}{
		{"model backends", sessionModelBackendsFailure, "Could not resolve model API backends"},
		{"billing", sessionBillingContextFailure, "Could not build session billing context"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("private provider failure")
			mapped := test.build(cause)
			if !errors.Is(mapped, cause) {
				t.Fatal("session context error lost its cause")
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v2/sessions/cse_test", nil)
			httpapi.NewErrorAdapter(nil).Write(response, request, mapped)
			if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "MCP") || strings.Contains(response.Body.String(), cause.Error()) {
				t.Fatalf("unexpected error detail: %s", response.Body.String())
			}
		})
	}
}

func TestWebToolContextErrors(t *testing.T) {
	for _, tc := range []struct {
		cause  error
		status int
	}{
		{errors.New("private database failure"), http.StatusInternalServerError},
		{db.ErrNotFound, http.StatusForbidden},
		{webToolDenied(), http.StatusForbidden},
	} {
		recorder := httptest.NewRecorder()
		httpapi.NewErrorAdapter(nil).Write(recorder, httptest.NewRequest(http.MethodGet, "/web/search", nil), webToolContextFailure(tc.cause))
		if recorder.Code != tc.status || strings.Contains(recorder.Body.String(), "private database failure") {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}
