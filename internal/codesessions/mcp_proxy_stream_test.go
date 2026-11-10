package codesessions

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestMCPProxySessionHeaderPreservesStreaming(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Values("X-OMA-Session-ID"); len(got) != 1 || got[0] != "sesn_test" {
			t.Errorf("session header=%q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Mcp-Session-Id", "upstream-session")
		_, _ = io.WriteString(w, "data: first\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: final\n\n")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)
	handler, token := newMCPProxyTestHandler(t, upstream.URL, slog.New(slog.DiscardHandler))
	handler.mcpProxyTransport = upstream.Client().Transport
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/v2/ccr-sessions/cse_test/mcp?"+url.Values{"mcp_url": {upstream.URL}}.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-OMA-Session-ID", "forged")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Mcp-Session-Id") != "upstream-session" {
		t.Fatalf("status=%d MCP session=%q", response.StatusCode, response.Header.Get("Mcp-Session-Id"))
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "data: first\n" {
		t.Fatalf("first event=%q err=%v", first, err)
	}
	close(release)
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "\ndata: final\n\n" {
		t.Fatalf("remaining events=%q err=%v", rest, err)
	}
}
