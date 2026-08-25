package platformapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"

	"github.com/go-chi/chi/v5"
)

func TestProxyMessagesHeadersAllowOnlyProtocolHeaders(t *testing.T) {
	source := http.Header{
		"Accept":              {"text/event-stream"},
		"Content-Type":        {"application/json"},
		"Anthropic-Version":   {"2023-06-01"},
		"Anthropic-Beta":      {"test-beta"},
		"Authorization":       {"Bearer browser-secret"},
		"Cookie":              {"sessionKey=browser-secret"},
		"X-Api-Key":           {"browser-api-key"},
		"X-Csrf-Token":        {"browser-csrf"},
		"X-Organization-Uuid": {"browser-org"},
		"X-Workspace-Id":      {"browser-workspace"},
		"Connection":          {"X-Connection-Only"},
		"X-Connection-Only":   {"browser-connection-secret"},
	}

	headers := proxyMessagesHeaders(source, "server-provider-key")
	if headers.Get("X-API-Key") != "server-provider-key" ||
		headers.Get("Authorization") != "Bearer server-provider-key" ||
		headers.Get("Accept") != "text/event-stream" ||
		headers.Get("Content-Type") != "application/json" ||
		headers.Get("Anthropic-Version") != "2023-06-01" ||
		headers.Get("Anthropic-Beta") != "test-beta" {
		t.Fatalf("protocol headers = %#v", headers)
	}
	if len(headers) != 6 {
		t.Fatalf("unexpected outbound headers = %#v", headers)
	}
}

func TestProxyMessagesModelUsesRealIDUnchanged(t *testing.T) {
	modelID, err := proxyMessagesModel([]byte(`{"model":"kimi-k2.5","max_tokens":16}`))
	if err != nil {
		t.Fatal(err)
	}
	if modelID != "kimi-k2.5" {
		t.Fatalf("model = %q, want kimi-k2.5", modelID)
	}
}

func TestProxyMessagesRejectsOversizedChunkedBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/organizations/org_test/proxy/v1/messages", nil)
	request.Body = io.NopCloser(io.LimitReader(repeatedByteReader(' '), llmproviders.MaxMessagesRequestBodyBytes+1))
	request.ContentLength = -1
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", "org_test")
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = auth.WithPrincipal(ctx, auth.Principal{OrganizationUUID: "org_test"})
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()

	handleProxyMessages(nil, nil, http.DefaultClient)(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", recorder.Code, recorder.Body.String())
	}
}

type repeatedByteReader byte

func (r repeatedByteReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = byte(r)
	}
	return len(buffer), nil
}

func TestProxyMessagesModelRejectsMissingModel(t *testing.T) {
	if _, err := proxyMessagesModel([]byte(`{"max_tokens":16}`)); err == nil {
		t.Fatal("proxyMessagesModel() error = nil")
	}
}

func TestProxyMessagesModelRejectsDuplicateModel(t *testing.T) {
	if _, err := proxyMessagesModel([]byte(`{"model":"configured","model":"not-configured"}`)); err == nil || err.Error() != "model must appear exactly once" {
		t.Fatalf("proxyMessagesModel() error = %v", err)
	}
}

func TestProxyProviderErrorDistinguishesMissingProviderFromLoadFailure(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
		wantMsg    string
	}{
		{
			name:       "model not configured",
			err:        llmproviders.ErrModelNotConfigured,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_request_error",
			wantMsg:    "Model is not configured for this workspace",
		},
		{
			name:       "provider missing",
			err:        llmproviders.ErrNotConfigured,
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "proxy_error",
			wantMsg:    "This workspace has no LLM provider configured",
		},
		{
			name:       "ambiguous model",
			err:        llmproviders.ErrAmbiguousModel,
			wantStatus: http.StatusInternalServerError,
			wantError:  "proxy_error",
			wantMsg:    "Workspace model configuration is unavailable",
		},
		{
			name:       "load failure",
			err:        errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantError:  "proxy_error",
			wantMsg:    "Workspace model configuration is unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeProxyProviderError(recorder, test.err)
			var response struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if recorder.Code != test.wantStatus || response.Error != test.wantError || response.Message != test.wantMsg {
				t.Fatalf("status=%d body=%#v", recorder.Code, response)
			}
		})
	}
}
