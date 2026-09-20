package vaults

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

type refreshWebhookRecorder struct{ events []webhooks.EnqueueInput }

func (r *refreshWebhookRecorder) Enqueue(_ context.Context, input webhooks.EnqueueInput) {
	r.events = append(r.events, input)
}

func TestOAuthRefreshFailureNotifications(t *testing.T) {
	for _, tc := range []struct {
		name                                             string
		status                                           int
		missing, winner, databaseFailure, networkFailure bool
		want                                             int
	}{
		{name: "timeout response is transient", status: 408},
		{name: "rate limit is transient", status: 429},
		{name: "server error is transient", status: 500},
		{name: "network failure is transient", status: 500, networkFailure: true},
		{name: "concurrent winner with missing refresh token", missing: true, winner: true},
		{name: "concurrent winner suppresses failure", status: 400, winner: true},
		{name: "database reload error suppresses notification", status: 400, databaseFailure: true},
		{name: "invalid grant notifies", status: 400, want: 1},
		{name: "rejected client notifies", status: 401, want: 1},
		{name: "missing refresh token notifies", missing: true, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			defer server.Close()
			if tc.networkFailure {
				server.Close()
			}
			svc := newTestSecretsService(t)
			refresh := "refresh-token"
			if tc.missing {
				refresh = ""
			}
			stale := sealedMCPOAuthCredential(t, svc, server.URL, "old-access", refresh, strPtr("2020-01-01T00:00:00Z"))
			latest := stale
			if tc.winner {
				latest = sealedMCPOAuthCredential(t, svc, server.URL, "winner-access", "new-refresh", strPtr("2099-01-01T00:00:00Z"))
			}
			store := &fakeCredentialStore{getResults: []db.VaultCredential{stale, latest}}
			if tc.databaseFailure {
				store.getErr = errors.New("database unavailable")
			}
			recorder := &refreshWebhookRecorder{}
			injector := newTestInjector(t, svc, store, server.Client(), oauthRefreshNow()).WithWebhooks(recorder)
			_, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, oauthRefreshNow(), false)
			if tc.winner && err != nil {
				t.Fatal(err)
			}
			if !tc.winner && err == nil {
				t.Fatal("expected refresh failure")
			}
			if len(recorder.events) != tc.want {
				t.Fatalf("notifications=%d want %d", len(recorder.events), tc.want)
			}
			if tc.want == 1 {
				event := recorder.events[0]
				if event.EventType != "vault_credential.refresh_failed" || event.ResourceID != stale.ExternalID || event.WorkspaceUUID != stale.WorkspaceUUID || event.WorkspaceExternalID != "workspace-test" || event.Options.VaultID == nil || *event.Options.VaultID != stale.VaultExternalID {
					t.Fatalf("wrong event metadata: %+v", event)
				}
			}
		})
	}
}

func TestOAuthSuccessfulExchangePersistenceFailureDoesNotNotify(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	stale := env.staleCred("refresh-token")
	recorder := &refreshWebhookRecorder{}
	injector := env.injector(&fakeCredentialStore{get: stale, updateErr: db.ErrVersionConflict}).WithWebhooks(recorder)
	if _, _, err := injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, false); err == nil {
		t.Fatal("expected persistence error")
	}
	if len(recorder.events) != 0 {
		t.Fatal("CAS exhaustion is not an OAuth rejection")
	}
}
