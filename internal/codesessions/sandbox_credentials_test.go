package codesessions

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSandboxCredentialRejectsInvalidScope(t *testing.T) {
	now := time.Now()
	credentials := newTestSessionCredentials(t, &now)
	identity := testSessionCredentialIdentity()
	if _, err := credentials.IssueSandbox(identity, ""); err == nil {
		t.Fatal("issued an unbound sandbox credential")
	}
	worker, err := credentials.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.VerifySandbox(worker); err == nil {
		t.Fatal("accepted a worker credential as a sandbox credential")
	}
	token, err := credentials.IssueSandbox(identity, "sandbox-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Verify(token); err == nil {
		t.Fatal("accepted a sandbox credential as a worker credential")
	}
	claims, err := credentials.VerifySandbox(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*SessionCredentialClaims){
		func(c *SessionCredentialClaims) { c.SandboxUUID = "" },
		func(c *SessionCredentialClaims) { c.WorkerEpoch = 1 },
		func(c *SessionCredentialClaims) { c.Role = "worker" },
	} {
		changed := claims
		change(&changed)
		invalid := signTestJWT(t, jwt.SigningMethodEdDSA, credentials.privateKey, credentials.kid, changed)
		if _, err := credentials.VerifySandbox(invalid); err == nil {
			t.Fatal("accepted incompatible sandbox credential claims")
		}
	}
	service := NewServiceWithCredentials(nil, credentials, nil)
	if _, err := service.authenticateRuntimeCredential(context.Background(), token, claims.SessionID); err == nil {
		t.Fatal("accepted a sandbox credential without lifecycle validation")
	}
}

func TestSandboxCredentialHasIndependentLifecycle(t *testing.T) {
	now := time.Now()
	credentials := newTestSessionCredentials(t, &now)
	identity := testSessionCredentialIdentity()
	identity.WorkerEpoch = 7
	token, err := credentials.IssueSandbox(identity, "sandbox-uuid")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := credentials.VerifySandbox(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != "sandbox" || claims.SandboxUUID != "sandbox-uuid" || claims.WorkerEpoch != 0 || claims.SessionID != identity.SessionID {
		t.Fatal("sandbox identity is not scoped independently of the worker")
	}
}
