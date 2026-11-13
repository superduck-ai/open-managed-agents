package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

func sessionPortServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	deps := ServerDeps{CodeSessionCredentials: credentials, FilestoreCredentials: filestoreCredentials}
	if enabled {
		codec, err := sessionports.NewSlugCodec(bytes.Repeat([]byte{3}, 32))
		if err != nil {
			t.Fatal(err)
		}
		deps.Config.SessionPorts = config.SessionPortsConfig{DomainSuffix: "preview.example.com", URLScheme: "https"}
		deps.SessionPorts = sessionports.NewWithCodec(deps.Config.SessionPorts, 3001, codec)
		deps.SessionPortRegistry = porttunnel.NewRegistry("test-owner", nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	return NewServer(deps)
}

func TestSessionPortTunnelRouteIsAbsentWhenDisabled(t *testing.T) {
	response := httptest.NewRecorder()
	sessionPortServer(t, false).ServeHTTP(response, httptest.NewRequest(http.MethodGet, porttunnel.Path, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
}

func TestSessionPortTunnelRouteUsesTunnelAuthentication(t *testing.T) {
	if porttunnel.Path != "/v2/sandbox/tunnel" {
		t.Fatalf("tunnel path = %s", porttunnel.Path)
	}
	server := sessionPortServer(t, true)
	for _, header := range []http.Header{
		{},
		{"Authorization": []string{"Bearer api-key-like-token"}},
		{"X-Api-Key": []string{"sk-ant-local-default"}, "X-Session-ID": []string{"cse_test"}},
	} {
		request := httptest.NewRequest(http.MethodGet, porttunnel.Path, nil)
		request.Header = header
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "Missing session port tunnel credential") {
			t.Fatalf("header=%v status=%d body=%s", header, response.Code, response.Body)
		}
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/sandbox/other", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("sibling path status=%d body=%s", response.Code, response.Body)
	}
}

func TestPublicRouterDoesNotExposeSessionPortForwarding(t *testing.T) {
	server := sessionPortServer(t, true)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/internal/session-ports/6f1c3f7e-0d1b-4f53-9d39-1b1e4f0e9a10/4000", strings.NewReader("")),
		httptest.NewRequest(http.MethodGet, "/internal/session-ports/6f1c3f7e-0d1b-4f53-9d39-1b1e4f0e9a10/4000/ws", nil),
	} {
		request.Header.Set("X-OMA-Port-Signature", "v1.0.forged")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", request.Method, request.URL.Path, response.Code, response.Body)
		}
	}
}
