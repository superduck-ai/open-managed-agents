package networkpolicy

import "testing"

func TestMCPURLHostRepresentationKeepsResourceBoundary(t *testing.T) {
	configured := "http://%E4%BE%8B%E5%AD%90.example.com:6788/mcp?tenant=A"
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"unrestricted"}`), AgentSnapshot: snapshotWithMCP(t, configured)}
	for _, tc := range []struct {
		target string
		allow  bool
	}{
		{"http://xn--fsqu00a.example.com:6788/mcp?tenant=B", false},
		{"http://child.xn--fsqu00a.example.com:6788/mcp?tenant=A", false},
		{"http://other.example.com:6788/mcp?tenant=A", false},
		{"http://xn--fsqu00a.example.com:6789/mcp?tenant=A", false},
		{"https://xn--fsqu00a.example.com:6788/mcp?tenant=A", false},
		{"http://xn--fsqu00a.example.com:6788/other?tenant=A", false},
		{"http://xn--fsqu00a.example.com.:6788/mcp?tenant=A", false},
		{"http://xn--fsqu00a.example.com:6788/mcp?tenant=A", true},
	} {
		got := authorizeMCPURLFixture(t, fixture, tc.target)
		if got.Allow != tc.allow {
			t.Fatalf("target=%s allow=%v reason=%s", tc.target, got.Allow, got.Reason)
		}
	}
}
