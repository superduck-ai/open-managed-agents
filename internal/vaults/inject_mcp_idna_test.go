package vaults

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestMCPIDNAHostEquivalencePreservesOtherBoundaries(t *testing.T) {
	for _, kind := range []string{"static_bearer", "mcp_oauth"} {
		key := credential(kind, "http://\u4f8b\u5b50.example.com:6788/mcp?tenant=A", "vlt_idna")
		for _, tc := range []struct {
			target string
			count  int
		}{
			{"http://other.example.com:6788/mcp?tenant=A", 0},
			{"http://child.xn--fsqu00a.example.com:6788/mcp?tenant=A", 0},
			{"http://xn--fsqu00a.example.com.:6788/mcp?tenant=A", 0},
			{"http://xn--fsqu00a.example.com:6788/other?tenant=A", 0},
			{"http://xn--fsqu00a.example.com:6788/mcp?tenant=B", 0},
			{"http://xn--fsqu00a.example.com:6789/mcp?tenant=A", 0},
			{"http://xn--fsqu00a.example.com:6788/mcp?tenant=A", 1},
		} {
			matches, _, err := listMCPInjectableMatches(mustURL(t, tc.target), []db.VaultCredential{key})
			if err != nil || len(matches) != tc.count {
				t.Fatalf("kind=%s target=%s matches=%d err=%v", kind, tc.target, len(matches), err)
			}
		}
		if matches, _, err := listInjectableMatches(mustURL(t, "http://xn--fsqu00a.example.com:6788/mcp?tenant=A"), []db.VaultCredential{key}); err != nil || len(matches) != 0 {
			t.Fatalf("generic host policy changed: matches=%d err=%v", len(matches), err)
		}
	}
}
