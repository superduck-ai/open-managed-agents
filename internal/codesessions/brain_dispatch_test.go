package codesessions

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWorkerRegistrationRejectsInvalidIdentity(t *testing.T) {
	for _, body := range []string{`{"session_id":"other"}`, `{"session_id":12}`, `{`} {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := validateCodeSessionWorkerRegisterBody(httptest.NewRecorder(), request, "cse_test"); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	for _, body := range []string{"", `{}`, `{"session_id":"cse_test"}`} {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := validateCodeSessionWorkerRegisterBody(httptest.NewRecorder(), request, "cse_test"); err != nil {
			t.Fatal("valid registration rejected")
		}
	}
}

func TestBrainClaimAuthenticatorRejectsInvalidTokens(t *testing.T) {
	directory := t.TempDir()
	shortFile := filepath.Join(directory, "short")
	if err := os.WriteFile(shortFile, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBrainClaimAuthenticator(shortFile); err == nil {
		t.Fatal("short brain claim token accepted")
	}
	if _, err := loadBrainClaimAuthenticator(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("missing brain claim token file accepted")
	}
	disabled, err := loadBrainClaimAuthenticator("")
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(directory, "token")
	token := strings.Repeat("a", 40)
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled, err := loadBrainClaimAuthenticator(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		authenticator brainClaimAuthenticator
		header        string
		ok            bool
	}{
		{"disabled", disabled, "Bearer " + token, false},
		{"missing", enabled, "", false},
		{"wrong scheme", enabled, "Basic " + token, false},
		{"wrong token", enabled, "Bearer " + strings.Repeat("b", 40), false},
		{"valid", enabled, "Bearer " + token, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", nil)
			request.Header.Set("Authorization", tc.header)
			if err := tc.authenticator.authorize(request); (err == nil) != tc.ok {
				t.Fatalf("authorize() = %v, want ok=%t", err, tc.ok)
			}
		})
	}
}

func TestParseBrainClaimBlock(t *testing.T) {
	for _, raw := range []string{"-1", "30001", "soon"} {
		if _, err := parseBrainClaimBlock(httptest.NewRequest("POST", "/?block_ms="+raw, nil)); err == nil {
			t.Fatalf("block_ms=%s accepted", raw)
		}
	}
	for raw, want := range map[string]time.Duration{"": 0, "0": 0, "25000": 25 * time.Second, "30000": 30 * time.Second} {
		got, err := parseBrainClaimBlock(httptest.NewRequest("POST", "/?block_ms="+raw, nil))
		if err != nil || got != want {
			t.Fatalf("block_ms=%q = %s, %v", raw, got, err)
		}
	}
}

func TestBrainClaimTransportSelectsShortLease(t *testing.T) {
	if usesBrainClaimTransport(db.CodeSession{Metadata: []byte(`{"config":{"worker_transport":"nats"}}`)}) ||
		usesBrainClaimTransport(db.CodeSession{Metadata: []byte(`not json`)}) || usesBrainClaimTransport(db.CodeSession{}) {
		t.Fatal("non-claim session treated as brain claim transport")
	}
	if !usesBrainClaimTransport(db.CodeSession{Metadata: []byte(`{"config":{"worker_transport":"claim"}}`)}) {
		t.Fatal("claim session not detected")
	}
	if got := leaseTimingsForTransport(""); got.ttl != 60*time.Second || got.grace != 10*time.Second {
		t.Fatalf("default lease = %+v", got)
	}
	if got := leaseTimingsForTransport("claim"); got.ttl != 20*time.Second || got.grace != 10*time.Second {
		t.Fatalf("claim lease = %+v", got)
	}
}
