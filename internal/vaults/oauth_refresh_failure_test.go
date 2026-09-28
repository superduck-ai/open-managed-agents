package vaults

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type refreshFailureProvider struct {
	secrets.KeyProvider
	wrapErr   error
	onFailure func()
}

func (p *refreshFailureProvider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	if p.wrapErr != nil {
		if p.onFailure != nil {
			p.onFailure()
		}
		return secrets.WrappedKey{}, p.wrapErr
	}
	return p.KeyProvider.WrapDEK(ctx, dek)
}

func TestRefreshSealFailureRequiresReauthorization(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	local, err := localkeys.New(localkeys.KeyMaterial{KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &refreshFailureProvider{KeyProvider: local}
	env.svc = secrets.NewService(provider)
	stale := env.staleCred("consumed-refresh")
	store := &fakeCredentialStore{get: stale}
	injector := env.injector(store)
	provider.wrapErr = errors.New("KMS wrap unavailable")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider.onFailure = cancel
	token, saved, err := injector.refreshMCPOAuthCredential(ctx, &stale, env.now, false)
	if !errors.Is(err, ErrMCPOAuthReauthorizationRequired) || token != "" || saved != nil {
		t.Fatal("failed seal returned an injectable token")
	}
	if env.tokenCalls.Load() != 1 {
		t.Fatal("test did not exchange the refresh token")
	}
	if store.get.SecretEnvelope != nil {
		t.Fatal("consumed refresh token remains persisted after sealing failure")
	}
	provider.wrapErr = nil
	_, _, err = injector.refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
	if err == nil || env.tokenCalls.Load() != 1 {
		t.Fatal("KMS recovery replayed consumed refresh token")
	}
}

func TestRefreshFailureDoesNotUseStaleSnapshot(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	stale := env.staleCred("old-refresh")
	store := &fakeCredentialStore{getErr: errors.New("database read failed")}
	_, _, err := env.injector(store).refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
	if err == nil || env.tokenCalls.Load() != 0 {
		t.Fatal("preload failure exchanged a stale refresh token")
	}
}

func TestRefreshSaveFailureInvalidatesOnlyExchangedEnvelope(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "same envelope", true: "concurrent reauthorization"}[replace], func(t *testing.T) {
			env := newOAuthRefreshEnv(t, "fresh-access")
			stale, winner := env.staleWinner("refresh", true)
			store := &fakeCredentialStore{get: stale, updateErr: errors.New("save failed"), getResults: []db.VaultCredential{stale}}
			if replace {
				store.get = winner
			}
			_, _, err := env.injector(store).refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
			if !errors.Is(err, ErrMCPOAuthReauthorizationRequired) || store.clearCalls != 1 {
				t.Fatalf("missing retirement: %v", err)
			}
			if (store.get.SecretEnvelope != nil) != replace {
				t.Fatal("retirement cleared the wrong envelope")
			}
		})
	}
}

func TestRefreshRetirementFailureIsReported(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	stale := env.staleCred("refresh")
	cleanupErr := errors.New("retirement database unavailable")
	store := &fakeCredentialStore{get: stale, updateErr: errors.New("save failed"), clearErr: cleanupErr}
	token, saved, err := env.injector(store).refreshMCPOAuthCredential(t.Context(), &stale, env.now, true)
	if token != "" || saved != nil || !errors.Is(err, cleanupErr) || !errors.Is(err, ErrMCPOAuthReauthorizationRequired) {
		t.Fatalf("lost cleanup failure: %v", err)
	}
}

func TestMissingOAuthEnvelopeKeepsHostBlocked(t *testing.T) {
	env := newOAuthRefreshEnv(t, "fresh-access")
	credential := env.staleCred("refresh")
	credential.SecretEnvelope = nil
	store := &fakeCredentialStore{get: credential}
	result, err := env.injector(store).resolveFromPlan(t.Context(), injectionPlan{hostCovered: true, matches: []*db.VaultCredential{&credential}}, nil, nil)
	if result != nil || !errors.Is(err, ErrInjectionRejected) || !errors.Is(err, ErrMCPOAuthReauthorizationRequired) {
		t.Fatalf("missing fail-closed reauthorization: %v", err)
	}
	if InjectionPublicMessage(err) != ErrMCPOAuthReauthorizationRequired.Error() {
		t.Fatal("public error omitted recovery action")
	}
	if env.tokenCalls.Load() != 0 {
		t.Fatal("missing envelope called token endpoint")
	}
}
