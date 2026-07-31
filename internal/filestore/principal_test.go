package filestore

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestPrincipalContextIsIsolatedFromGlobalAuth(t *testing.T) {
	globalContext := auth.WithPrincipal(context.Background(), auth.Principal{WorkspaceUUID: "22222222-2222-4222-8222-222222222222"})
	if _, ok := PrincipalFromContext(globalContext); ok {
		t.Fatal("global auth principal was accepted as a Filestore principal")
	}

	filestoreContext := WithPrincipal(context.Background(), Principal{WorkspaceUUID: "22222222-2222-4222-8222-222222222222"})
	if _, ok := auth.PrincipalFromContext(filestoreContext); ok {
		t.Fatal("Filestore principal leaked into the global auth context")
	}
	principal, ok := PrincipalFromContext(filestoreContext)
	if !ok || principal.WorkspaceUUID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("Filestore principal = %#v, %v", principal, ok)
	}
}
