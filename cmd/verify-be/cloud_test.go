package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestCloudConfigRequiresExplicitPrivateConfiguration(t *testing.T) {
	t.Setenv("VERIFY_BE_CLOUD_CONFIG", "")
	if _, err := loadCloudConfig(cliOptions{Scenario: "files.cloud-storage"}); err == nil {
		t.Fatal("missing config accepted")
	}
	path := filepath.Join(t.TempDir(), "cloud.yaml")
	for _, data := range []string{"purpose: production", "purpose: verify-be\nstorage: {}", "purpose: verify-be\nunknown: secret"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadCloudConfig(cliOptions{Scenario: "files.cloud-storage", CloudConfig: path}); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid config accepted or content exposed")
		}
	}
	for _, endpoint := range []string{"http://cloud.test", "https://user:pass@cloud.test", "https://cloud.test?token=secret"} {
		if cloudEndpoint(endpoint) {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	data := "purpose: verify-be\ne2b:\n  api_key: test\n  template: base\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCloudConfig(cliOptions{Scenario: "chat.cloud-renewal", CloudConfig: path}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCloudConfig(cliOptions{Scenario: "chat.cloud-renewal", CloudConfig: path}); err == nil {
		t.Fatal("public config accepted")
	}
}

func TestCloudStorageAssertionsAndCleanup(t *testing.T) {
	for _, readonlyEnforced := range []bool{false, true} {
		t.Run(fmt.Sprint(readonlyEnforced), func(t *testing.T) {
			var mu sync.Mutex
			var object []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Query().Has("versioning") {
					w.Header().Set("Content-Type", "application/xml")
					_, _ = io.WriteString(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`)
					return
				}
				if !strings.HasPrefix(r.URL.Path, "/test/verify-be/run-test/") {
					t.Error("operation escaped run prefix")
					http.NotFound(w, r)
					return
				}
				if readonlyEnforced && strings.Contains(r.Header.Get("Authorization"), "Credential=reader/") && r.Method != "GET" {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(403)
					_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code></Error>`)
					return
				}
				switch r.Method {
				case "PUT":
					object, _ = io.ReadAll(r.Body)
					w.Header().Set("ETag", `"test"`)
				case "GET":
					if object == nil {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(404)
						_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
						return
					}
					_, _ = w.Write(object)
				case "DELETE":
					object = nil
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			cfg := cloudConfig{Storage: config.S3Config{Endpoint: server.URL, Bucket: "test", Region: "us-east-1", AccessKeyID: "owner", SecretAccessKey: "secret", ForcePathStyle: true}}
			cfg.ReadOnly.AccessKeyID, cfg.ReadOnly.SecretAccessKey = "reader", "secret"
			r := report{RunID: "run-test"}
			var stages []string
			err := verifyCloudStorage(t.Context(), cfg, t.TempDir(), &r, func(s string) { stages = append(stages, s) })
			if (err == nil) != readonlyEnforced || !r.CleanupComplete {
				t.Fatalf("verification error=%v cleanup=%v", err, r.CleanupComplete)
			}
			if readonlyEnforced && len(stages) != 3 {
				t.Fatal("missing cloud stages")
			}
			mu.Lock()
			defer mu.Unlock()
			if object != nil {
				t.Fatal("cloud object leaked")
			}
		})
	}
}

func TestCloudRenewalRejectsUnchangedDeadlineAndCleansSandbox(t *testing.T) {
	for _, extend := range []bool{false, true} {
		t.Run(fmt.Sprint(extend), func(t *testing.T) {
			var mu sync.Mutex
			deadline := time.Now().Add(time.Minute)
			deleted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "DELETE":
					deleted = true
					w.WriteHeader(204)
				case r.URL.Path == "/sandboxes/sbx_test/timeout":
					var body struct {
						Timeout int `json:"timeout"`
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body.Timeout != 180 {
						t.Error("wrong renewal duration")
					}
					if extend {
						deadline = time.Now().Add(3 * time.Minute)
					}
					_, _ = io.WriteString(w, `{}`)
				case r.Method == "GET":
					if deleted {
						http.Error(w, `{"message":"not found"}`, 404)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"sandboxID": "sbx_test", "templateID": "base", "startedAt": time.Now(), "endAt": deadline, "state": "running"})
				default:
					_, _ = io.WriteString(w, `{"sandboxID":"sbx_test","templateID":"base","envdVersion":"0.1.0","envdURL":"http://127.0.0.1:1"}`)
				}
			}))
			defer server.Close()
			cfg := cloudConfig{E2B: config.E2BConfig{APIKey: "e2b_0000000000000000000000000000000000000000", APIURL: server.URL, SandboxURL: server.URL, Template: "base", RequestTimeout: time.Second, SandboxTimeout: time.Minute}}
			r := report{RunID: "run-test"}
			var stages []string
			err := verifyCloudRenewal(t.Context(), cfg, t.TempDir(), &r, func(s string) { stages = append(stages, s) })
			if (err == nil) != extend || !r.CleanupComplete {
				t.Fatalf("renewal error=%v cleanup=%v", err, r.CleanupComplete)
			}
			if extend && len(stages) != 3 {
				t.Fatal("missing renewal stages")
			}
			mu.Lock()
			defer mu.Unlock()
			if !deleted {
				t.Fatal("sandbox leaked")
			}
		})
	}
}
