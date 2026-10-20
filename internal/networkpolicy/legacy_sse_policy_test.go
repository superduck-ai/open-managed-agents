package networkpolicy

import "testing"

func TestLegacySSESessionQueryKeepsDeclaredEndpointBoundary(t *testing.T) {
	policy, err := ParseMCPProxyPolicy([]byte(`{"type":"cloud","networking":{"type":"limited","allow_mcp_servers":true}}`), []byte(`{"mcp_servers":[{"name":"peer","type":"url","url":"https://peer.example/mcp?tenant=a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"https://peer.example/mcp?tenant=b&sessionid=owned", "https://peer.example/other?tenant=a&sessionid=owned", "https://other.example/mcp?tenant=a&sessionid=owned",
		"https://peer.example/mcp?tenant=a&sessionid=", "https://peer.example/mcp?tenant=a&sessionid=one&sessionid=two", "https://peer.example/mcp?tenant=a&sessionid=owned&extra=true",
		"https://peer.example:8443/mcp?tenant=a&sessionid=owned", "https://peer.example/mcp?tenant=a&sessionid=owned#fragment",
	} {
		if policy.AuthorizeMCPURL(target).Allow {
			t.Fatalf("unexpected endpoint authority: %s", target)
		}
	}
	for _, target := range []string{"https://peer.example/mcp?tenant=a", "https://peer.example/mcp?tenant=a&sessionid=owned", "https://peer.example/mcp?sessionid=owned&tenant=a"} {
		if !policy.AuthorizeMCPURL(target).Allow {
			t.Fatalf("actual legacy message endpoint denied: %s", target)
		}
	}
}
