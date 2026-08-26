package tunnels

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestValidateTunnelResponseRejectsInvalidHTTPStatus(t *testing.T) {
	t.Parallel()
	response := &TunnelResponse{
		RequestID:    "req_0123456789AbCdEfGhIjKlMn",
		ResponseCode: 999,
		ResponseType: ResponseTypeJSONRPC,
		JSONResponse: []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
	}
	if err := validateTunnelResponse(response, configForHeaderValidation()); err == nil {
		t.Fatal("validateTunnelResponse accepted invalid resp_code")
	}
	response.ResponseCode = http.StatusOK
	if err := validateTunnelResponse(response, configForHeaderValidation()); err != nil {
		t.Fatalf("validateTunnelResponse valid status: %v", err)
	}
}

func TestValidateTunnelResponseSanitizesOAuthTransportHeaders(t *testing.T) {
	t.Parallel()
	response := &TunnelResponse{
		RequestID:       "req_0123456789AbCdEfGhIjKlMn",
		ResponseCode:    http.StatusOK,
		ResponseType:    ResponseTypeOAuth,
		JSONResponse:    []byte(`{"resource":"https://internal.example/mcp"}`),
		ResponseHeaders: http.Header{"Content-Length": {"57"}, "Content-Type": {"application/json"}, "Date": {"Tue, 25 Aug 2026 03:12:51 GMT"}, "Cache-Control": {"no-store"}},
	}

	if err := validateTunnelResponse(response, configForHeaderValidation()); err != nil {
		t.Fatalf("validateTunnelResponse OAuth headers: %v", err)
	}
	if response.ResponseHeaders.Get("Content-Length") != "" || response.ResponseHeaders.Get("Date") != "" {
		t.Fatalf("transport headers were not removed: %#v", response.ResponseHeaders)
	}
	if response.ResponseHeaders.Get("Cache-Control") != "no-store" {
		t.Fatalf("OAuth cache header was not preserved: %#v", response.ResponseHeaders)
	}
}

func TestValidateTunnelResponseRejectsUntrustedOAuthHeader(t *testing.T) {
	t.Parallel()
	response := &TunnelResponse{
		RequestID:       "req_0123456789AbCdEfGhIjKlMn",
		ResponseCode:    http.StatusOK,
		ResponseType:    ResponseTypeOAuth,
		JSONResponse:    []byte(`{"resource":"https://internal.example/mcp"}`),
		ResponseHeaders: http.Header{"Set-Cookie": {"secret=value"}},
	}

	if err := validateTunnelResponse(response, configForHeaderValidation()); err == nil {
		t.Fatal("validateTunnelResponse accepted Set-Cookie from OAuth discovery")
	}
}

func configForHeaderValidation() config.TunnelConfig {
	return config.TunnelConfig{MaxHeaderBytes: 32 << 10, MaxHeaderValueBytes: 8 << 10}
}

func TestHasTunnelBetaAcceptsCurrentAndLegacyValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		want   bool
	}{
		{name: "missing", values: nil, want: false},
		{name: "unrelated", values: []string{"files-api-2025-04-14"}, want: false},
		{name: "current", values: []string{currentBeta}, want: true},
		{name: "legacy", values: []string{legacyBeta}, want: true},
		{name: "comma separated", values: []string{"files-api-2025-04-14, " + currentBeta}, want: true},
		{name: "multiple headers", values: []string{"files-api-2025-04-14", legacyBeta}, want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := hasTunnelBeta(testCase.values); got != testCase.want {
				t.Fatalf("hasTunnelBeta() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestManagementInputValidationFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "empty display name",
			run: func() error {
				value := "  "
				_, err := normalizeDisplayName(&value)
				return err
			},
		},
		{
			name: "display name too long",
			run: func() error {
				value := strings.Repeat("界", 256)
				_, err := normalizeDisplayName(&value)
				return err
			},
		},
		{
			name: "invalid limit",
			run: func() error {
				_, err := parseListLimit(httptest.NewRequest("GET", "/?limit=1001", nil))
				return err
			},
		},
		{
			name: "invalid archived flag",
			run: func() error {
				_, err := parseIncludeArchived(httptest.NewRequest("GET", "/?include_archived=sometimes", nil))
				return err
			},
		},
		{
			name: "invalid cursor",
			run: func() error {
				_, err := parseCursor("not-base64!")
				return err
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if err := testCase.run(); err == nil {
				t.Fatal("validation succeeded, want error")
			}
		})
	}
}

func TestTunnelCursorRoundTrip(t *testing.T) {
	t.Parallel()
	cursor, err := marshalCursor(42)
	if err != nil {
		t.Fatalf("marshalCursor: %v", err)
	}
	offset, err := parseCursor(cursor)
	if err != nil {
		t.Fatalf("parseCursor: %v", err)
	}
	if offset != 42 {
		t.Fatalf("offset = %d, want 42", offset)
	}
}
