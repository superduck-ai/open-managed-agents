package devinit

import (
	"strings"
	"testing"
)

func TestEnvironmentRequiresReservedIdentity(t *testing.T) {
	state := State{Workspaces: []WorkspaceState{{Ref: "default"}}}
	if err := reconcileEnvironments(t.Context(), nil, "", &state, Options{}); err == nil || !strings.Contains(err.Error(), "identity has not been reserved") {
		t.Fatalf("unreserved environment: %v", err)
	}
}
