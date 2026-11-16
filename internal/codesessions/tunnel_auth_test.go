package codesessions

import (
	"errors"
	"fmt"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

func TestTunnelCredentialFailureKeepsTransientErrorsRetryable(t *testing.T) {
	transient := fmt.Errorf("%w: %w", errRuntimeCredentialUnavailable, errors.New("connection reset"))
	if errors.Is(tunnelCredentialFailure(transient), porttunnel.ErrUnauthorized) {
		t.Fatal("transient validation failure closed the tunnel")
	}
	if errors.Is(tunnelCredentialFailure(errors.New("db down")), porttunnel.ErrUnauthorized) {
		t.Fatal("unknown failure treated as revoked")
	}
}

func TestTunnelCredentialFailureRejectsRevokedCredentials(t *testing.T) {
	for name, cause := range map[string]error{
		"invalid token":       fmt.Errorf("%w: %w", errRuntimeCredentialInvalid, errors.New("bad signature")),
		"identity inactive":   errSessionIngressIdentity,
		"worker epoch":        db.ErrWorkerEpochMismatch,
		"code session absent": db.ErrNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			if err := tunnelCredentialFailure(cause); !errors.Is(err, porttunnel.ErrUnauthorized) || !errors.Is(err, cause) {
				t.Fatalf("failure = %v", err)
			}
		})
	}
}
