package tunnels

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMapTunnelLookupErrorMapsConcurrentMutationToConflict(t *testing.T) {
	t.Parallel()
	err := mapTunnelLookupError(db.ErrInvalidState, "tunnel_test", "rotate token for")
	var appError *apperr.Error
	if !errors.As(err, &appError) {
		t.Fatalf("mapTunnelLookupError error = %T, want *apperr.Error", err)
	}
	if appError.Kind != apperr.Conflict {
		t.Fatalf("mapTunnelLookupError kind = %v, want conflict", appError.Kind)
	}
}

func TestTokenTransitionErrorMapsRetiredVersionToConflict(t *testing.T) {
	t.Parallel()
	err := tokenTransitionError("Could not rotate tunnel token", ErrTokenRetired)
	var appError *apperr.Error
	if !errors.As(err, &appError) {
		t.Fatalf("tokenTransitionError error = %T, want *apperr.Error", err)
	}
	if appError.Kind != apperr.Conflict {
		t.Fatalf("tokenTransitionError kind = %v, want conflict", appError.Kind)
	}
}
