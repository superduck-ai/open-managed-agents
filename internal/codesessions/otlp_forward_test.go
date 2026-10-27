package codesessions

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestOpenObserveOTLPForwarderUsesServerOwnedContract(t *testing.T) {
	const payload = `{"resourceLogs":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oma/v1/logs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "ingest-user" || password != "ingest-password" {
			t.Errorf("BasicAuth() = %q %q %t", username, password, ok)
		}
		if r.Header.Get("stream-name") != "oma_claude_code" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"partialSuccess":{"rejectedLogRecords":"1","errorMessage":"invalid record"}}`))
	}))
	defer server.Close()

	forwarder := newOpenObserveOTLPForwarder(config.ObservabilityConfig{
		Enabled: true,
		OpenObserve: config.OpenObserveConfig{
			BaseURL:      server.URL,
			Organization: "oma",
			LogsStream:   "oma_claude_code",
			Ingestion:    config.BackendCredentialsConfig{Username: "ingest-user", Password: "ingest-password"},
		},
		OTLP: config.ObservabilityOTLPConfig{ForwardTimeout: time.Second},
	})
	response, err := forwarder.forward(context.Background(), "logs", otlpProtocolJSON, []byte(payload))
	if err != nil {
		t.Fatalf("forward() error = %v", err)
	}
	if len(response.body) == 0 {
		t.Fatal("partial success response was discarded")
	}
}

func TestOpenObserveOTLPForwarderRedactsUpstreamErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "sensitive upstream detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	forwarder := newOpenObserveOTLPForwarder(config.ObservabilityConfig{
		Enabled:     true,
		OpenObserve: config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma"},
		OTLP:        config.ObservabilityOTLPConfig{ForwardTimeout: time.Second},
	})

	_, err := forwarder.forward(context.Background(), "metrics", otlpProtocolProtobuf, nil)
	var sinkError *otlpSinkError
	if !errors.As(err, &sinkError) || sinkError.statusCode != http.StatusServiceUnavailable {
		t.Fatalf("forward() error = %#v", err)
	}
	if strings.Contains(err.Error(), "sensitive upstream detail") {
		t.Fatalf("forward() leaked upstream response body: %v", err)
	}
}

func TestOpenObserveOTLPForwarderPreservesRetrySemantics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer server.Close()
	forwarder := newOpenObserveOTLPForwarder(config.ObservabilityConfig{
		Enabled:     true,
		OpenObserve: config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma"},
		OTLP:        config.ObservabilityOTLPConfig{ForwardTimeout: time.Second},
	})

	_, err := forwarder.forward(context.Background(), "metrics", otlpProtocolProtobuf, nil)
	var sinkError *otlpSinkError
	if !errors.As(err, &sinkError) || sinkError.statusCode != http.StatusTooManyRequests || sinkError.retryAfter != "7" {
		t.Fatalf("forward() error = %#v", err)
	}
}
