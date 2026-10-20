package vaults

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestInjectionExhaustionMarksOnlyRemotelyRejectedCandidates(t *testing.T) {
	svc := newTestSecretsService(t)
	a := sealedStaticBearerCredential(t, svc, testInjectMCPURL, "local-a", "cred_a")
	b := sealedStaticBearerCredential(t, svc, testInjectMCPURL, "local-b", "cred_b")
	injector := newTestInjector(t, svc, nil, nil, time.Time{})
	for _, test := range []struct {
		name               string
		matches            []*db.VaultCredential
		excluded           map[string]struct{}
		wantAuthentication bool
	}{
		{name: "all remotely rejected", matches: []*db.VaultCredential{&a, &b}, excluded: map[string]struct{}{"cred_a": {}, "cred_b": {}}, wantAuthentication: true},
		{name: "covered without matches", excluded: map[string]struct{}{}},
		{name: "unreadable secret", matches: []*db.VaultCredential{{ExternalID: "broken", AuthType: "static_bearer"}}, excluded: map[string]struct{}{}},
		{name: "remote rejection plus unreadable secret", matches: []*db.VaultCredential{&a, {ExternalID: "broken", AuthType: "static_bearer"}}, excluded: map[string]struct{}{"cred_a": {}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := injector.resolveFromPlan(context.Background(), injectionPlan{matches: test.matches, hostCovered: true}, test.excluded, nil)
			if !errors.Is(err, ErrInjectionRejected) {
				t.Fatal("missing fail-closed injection rejection")
			}
			if got := errors.Is(err, ErrUpstreamAuthenticationRejected); got != test.wantAuthentication {
				t.Fatalf("authentication classification = %v", got)
			}
		})
	}
}
