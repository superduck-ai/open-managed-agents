package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

func TestOptionalObservabilityDoesNotBlockServer(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfigured", true: "missing credentials"}[enabled], func(t *testing.T) {
			var logs bytes.Buffer
			server := optionalObservabilityServer(t, config.ObservabilityConfig{Enabled: enabled}, &logs)
			if server.cfg.Observability.Enabled {
				t.Fatal("incomplete observability configuration remained enabled")
			}
			if enabled {
				entries := parseSlogJSONLines(t, logs.String())
				if len(entries) != 1 || entries[0]["level"] != "WARN" || entries[0]["msg"] != "observability disabled" || entries[0]["component"] != "observability" || entries[0]["error"] == nil {
					t.Fatalf("missing structured degradation warning: %#v", entries)
				}
			}
			assertOptionalObservabilityHealth(t, server)
		})
	}
}

func TestUnavailableOpenObserveIsNotAStartupProbe(t *testing.T) {
	var calls atomic.Int32
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer storage.Close()
	var logs bytes.Buffer
	server := optionalObservabilityServer(t, config.ObservabilityConfig{
		Enabled: true, Backend: config.ObservabilityBackendOpenObserve,
		OTLP: config.ObservabilityOTLPConfig{MaxRequestBytes: 1024, ForwardTimeout: time.Second},
		OpenObserve: config.OpenObserveConfig{
			BaseURL: storage.URL, Organization: "oma", LogsStream: "oma_dsh",
			Ingestion: config.BackendCredentialsConfig{Username: "writer", Password: "secret"},
			Query:     config.BackendQueryConfig{Username: "reader", Password: "secret", Timeout: time.Second},
		},
	}, &logs)
	if !server.cfg.Observability.Enabled {
		t.Fatal("valid optional configuration was disabled before any request")
	}
	assertOptionalObservabilityHealth(t, server)
	if calls.Load() != 0 {
		t.Fatal("server startup or health depends on OpenObserve")
	}
}

func optionalObservabilityServer(t *testing.T, obs config.ObservabilityConfig, logs *bytes.Buffer) *Server {
	t.Helper()
	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(ServerDeps{
		Config:                 config.Config{Observability: obs},
		Logger:                 slog.New(slog.NewJSONHandler(logs, nil)),
		CodeSessionCredentials: credentials, FilestoreCredentials: files,
	})
}

func assertOptionalObservabilityHealth(t *testing.T, server *Server) {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health failed: %d", response.Code)
	}
}
