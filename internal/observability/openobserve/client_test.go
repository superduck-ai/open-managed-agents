package openobserve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestSearchClientSuccessRequestShape(t *testing.T) {
	var captured struct {
		auth  string
		typ   string
		start int64
		end   int64
		sql   string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.auth = r.Header.Get("Authorization")
		captured.typ = r.URL.Query().Get("type")
		body, _ := io.ReadAll(r.Body)
		var payload searchRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		captured.start = payload.Query.StartTime
		captured.end = payload.Query.EndTime
		captured.sql = payload.Query.SQL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"current":1}]}`))
	}))
	defer server.Close()

	client := newSearchClient(config.OpenObserveConfig{
		BaseURL:      server.URL,
		Organization: "oma",
		Query:        config.BackendQueryConfig{Username: "query-user", Password: "query-pass", Timeout: time.Second},
	}, nil, server.Client())
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	hits, err := client.search(context.Background(), streamTraces, "SELECT 1", start, end, 1000)
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %#v", hits)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("query-user:query-pass"))
	if captured.auth != wantAuth {
		t.Fatalf("Authorization = %q, want %q", captured.auth, wantAuth)
	}
	if captured.typ != "traces" || captured.start != start.UnixMicro() || captured.end != end.UnixMicro() {
		t.Fatalf("captured = %+v", captured)
	}
	if strings.Contains(captured.auth, "query-pass") && !strings.HasPrefix(captured.auth, "Basic ") {
		t.Fatal("password leaked outside basic auth")
	}
}

func TestSearchClientErrorMapping(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"hits":[]}`))
		}))
		defer server.Close()
		client := newSearchClient(config.OpenObserveConfig{
			BaseURL: server.URL, Organization: "oma",
			Query: config.BackendQueryConfig{Username: "u", Password: "p", Timeout: 10 * time.Millisecond},
		}, nil, &http.Client{Timeout: 10 * time.Millisecond})
		_, err := client.search(context.Background(), streamMetrics, "SELECT 1", time.Now().Add(-time.Hour), time.Now(), 10)
		assertKind(t, err, apperr.Timeout)
	})
	t.Run("unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"secret-body"}`))
		}))
		defer server.Close()
		client := clientFor(server)
		_, err := client.search(context.Background(), streamLogs, "SELECT 1", time.Now().Add(-time.Hour), time.Now(), 10)
		assertKind(t, err, apperr.Unavailable)
		if strings.Contains(err.Error(), "secret-body") {
			t.Fatalf("error leaked body: %v", err)
		}
	})
	t.Run("internal-4xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"sql broken"}`))
		}))
		defer server.Close()
		_, err := clientFor(server).search(context.Background(), streamTraces, "SELECT 1", time.Now().Add(-time.Hour), time.Now(), 10)
		assertKind(t, err, apperr.Internal)
	})
	t.Run("oversize", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(bytesRepeat(maxSearchResponseBytes + 2))
		}))
		defer server.Close()
		_, err := clientFor(server).search(context.Background(), streamTraces, "SELECT 1", time.Now().Add(-time.Hour), time.Now(), 10)
		assertKind(t, err, apperr.Internal)
	})
}

func clientFor(server *httptest.Server) *searchClient {
	return newSearchClient(config.OpenObserveConfig{
		BaseURL: server.URL, Organization: "oma",
		Query: config.BackendQueryConfig{Username: "u", Password: "p", Timeout: time.Second},
	}, nil, server.Client())
}

func bytesRepeat(n int) []byte {
	return []byte(strings.Repeat("a", n))
}

func assertKind(t *testing.T, err error, kind apperr.Kind) {
	t.Helper()
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != kind {
		t.Fatalf("error = %v, want kind %v", err, kind)
	}
	if strings.Contains(appErr.PublicMessage, "secret") || strings.Contains(appErr.PublicMessage, "sql broken") {
		t.Fatalf("public message leaked body: %q", appErr.PublicMessage)
	}
}

func TestBackendStatSearchWindowIncludesPreviousPeriod(t *testing.T) {
	var captured searchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"current":2,"previous":1,"change_percent":100}]}`))
	}))
	defer server.Close()
	backend := NewWithHTTPClient(config.OpenObserveConfig{
		BaseURL: server.URL, Organization: "oma",
		Query: config.BackendQueryConfig{Username: "u", Password: "p", Timeout: time.Second},
	}, nil, server.Client())
	bound := testBound()
	if _, err := backend.PanelRows(context.Background(), "overview.interactions", bound); err != nil {
		t.Fatalf("PanelRows() error = %v", err)
	}
	if captured.Query.StartTime != bound.Window.PrevStart.UnixMicro() || captured.Query.EndTime != bound.Window.End.UnixMicro() {
		t.Fatalf("stat window start=%d end=%d, want prev_start=%d end=%d", captured.Query.StartTime, captured.Query.EndTime, bound.Window.PrevStart.UnixMicro(), bound.Window.End.UnixMicro())
	}

	if _, err := backend.PanelRows(context.Background(), "overview.llm_request_trend", bound); err != nil {
		t.Fatalf("timeseries PanelRows() error = %v", err)
	}
	if captured.Query.StartTime != bound.Window.Start.UnixMicro() {
		t.Fatalf("timeseries window start=%d, want current start=%d", captured.Query.StartTime, bound.Window.Start.UnixMicro())
	}
}

func TestBackendPanelRowsUsesRenderedSQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "service_oma_organization_uuid") {
			t.Errorf("sql missing traces scope: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"current":2,"previous":1,"change_percent":100}]}`))
	}))
	defer server.Close()
	backend := NewWithHTTPClient(config.OpenObserveConfig{
		BaseURL: server.URL, Organization: "oma",
		Query: config.BackendQueryConfig{Username: "u", Password: "p", Timeout: time.Second},
	}, nil, server.Client())
	rows, err := backend.PanelRows(context.Background(), "overview.interactions", testBound())
	if err != nil {
		t.Fatalf("PanelRows() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
}
