package vaults

import (
	"errors"
	"testing"
	"time"
)

func TestMissingOAuthRefreshRequiresAuthenticationRecovery(t *testing.T) {
	for _, row := range []struct {
		name           string
		expired, force bool
	}{{"expired", true, false}, {"remotely rejected", false, true}, {"usable", false, false}} {
		t.Run(row.name, func(t *testing.T) {
			env := newOAuthRefreshEnv(t, "unused-new-token")
			expiry := env.now.Add(time.Hour).Format(time.RFC3339)
			if row.expired {
				expiry = env.now.Add(-time.Hour).Format(time.RFC3339)
			}
			auth := testMCPOAuthPublicAuth(env.tokenURL, &expiry)
			auth.Refresh = nil
			secret := testMCPOAuthSecret("access-only", "")
			secret.Refresh = nil
			cred := sealMCPOAuthCredential(t, env.svc, auth, secret, "access-only")
			store := &fakeCredentialStore{get: cred}
			result, err := env.injector(store).resolveMCPOAuthToken(t.Context(), &cred, row.force)
			if row.expired || row.force {
				if result != nil || !errors.Is(err, ErrMCPOAuthReauthorizationRequired) || !errors.Is(err, errMCPOAuthRefreshTokenMissing) {
					t.Fatalf("missing-refresh authentication classification: %v", err)
				}
			} else if err != nil || result == nil || result.token != "access-only" {
				t.Fatal("valid access-only token unusable")
			}
			if env.tokenCalls.Load() != 0 || store.updateCalls != 0 {
				t.Fatal("missing refresh invented token exchange or mutation")
			}
		})
	}
}
