package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestLocalSandboxAPIRejectsUnownedResources(t *testing.T) {
	calls := fakeDocker(t)
	server := newLocalSandboxAPI("owned-run")
	defer server.Close()
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/sandboxes/foreign-sandbox/connect"},
		{http.MethodPost, "/sandboxes/oma-public-other-run-1/timeout"},
		{http.MethodDelete, "/sandboxes/foreign-sandbox"},
		{http.MethodDelete, "/sandboxes/oma-public-owned-run-foreign"},
	} {
		request, err := http.NewRequestWithContext(t.Context(), tc.method, server.URL+tc.path, strings.NewReader(`{"timeout":60}`))
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s returned %d, want 404", tc.method, tc.path, response.StatusCode)
		}
	}
	data, err := os.ReadFile(calls)
	if err != nil || strings.Contains(string(data), "rm -f") {
		t.Fatalf("unowned sandbox deletion: calls=%s error=%v", data, err)
	}
}

func TestLocalSandboxAPIOwnedLifecycle(t *testing.T) {
	calls := fakeDocker(t)
	server := newLocalSandboxAPI("0")
	defer server.Close()
	for _, tc := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/connect", `{"timeout":0}`, http.StatusBadRequest},
		{http.MethodPost, "/connect", `{"timeout":60}`, http.StatusOK},
		{http.MethodPost, "/timeout", `{"timeout":60}`, http.StatusOK},
		{http.MethodDelete, "", "", http.StatusNoContent},
	} {
		request, err := http.NewRequestWithContext(t.Context(), tc.method, server.URL+"/sandboxes/oma-public-0-1"+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if tc.path == "/connect" && tc.status == http.StatusOK {
			var connection struct {
				SandboxID string `json:"sandboxID"`
			}
			if err := json.NewDecoder(response.Body).Decode(&connection); err != nil || connection.SandboxID != "oma-public-0-1" {
				t.Fatalf("connect response=%+v error=%v", connection, err)
			}
		}
		_ = response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("%s %s returned %d, want %d", tc.method, tc.path, response.StatusCode, tc.status)
		}
	}
	data, err := os.ReadFile(calls)
	if err != nil || !strings.Contains(string(data), "rm -f oma-public-0-1") {
		t.Fatalf("owned sandbox deletion: calls=%s error=%v", data, err)
	}
}

func TestLocalSandboxConfigSupportsProviderRenewal(t *testing.T) {
	fakeDocker(t)
	directory := t.TempDir()
	env := newEnvironment(directory, directory, "0")
	env.publicSandbox = true
	defer env.close()
	if err := env.writeConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		E2B struct {
			APIKey     string `json:"api_key"`
			APIURL     string `json:"api_url"`
			SandboxURL string `json:"sandbox_url"`
		} `json:"e2b"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	provider := e2bruntime.NewProvider(config.E2BConfig{
		APIKey: cfg.E2B.APIKey, APIURL: cfg.E2B.APIURL, SandboxURL: cfg.E2B.SandboxURL,
		RequestTimeout: time.Second,
	})
	if err := provider.SetTimeout(t.Context(), "oma-public-0-1", time.Minute); err != nil {
		t.Fatal(err)
	}
}
