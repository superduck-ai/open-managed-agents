package vaults

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestArchivedCredentialDiagnosticDoesNotOpenOrUseSecret(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	svc := newTestSecretsService(t)
	for _, purged := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained envelope", true: "purged envelope"}[purged], func(t *testing.T) {
			credential := sealedMCPOAuthCredential(t, svc, server.URL, "archived-access", "archived-refresh", nil)
			archived := time.Now().UTC()
			credential.ArchivedAt = &archived
			if purged {
				credential.SecretEnvelope = nil
			}
			store := &fakeCredentialStore{get: credential}
			injector := newTestInjector(t, svc, store, server.Client(), oauthRefreshNow())
			_, err := injector.validateMCPOAuth(t.Context(), credential)
			if !errors.Is(err, errArchivedDiagnostic) {
				t.Fatalf("wanted archived rejection, got %v", err)
			}
			if errors.Is(err, ErrMissingSecretEnvelope) || calls.Load() != 0 || store.updateCalls != 0 {
				t.Fatal("archived diagnostic accessed secret or downstream service")
			}
		})
	}
}
