package api

import "testing"

func TestPlatformMCPLogHostOmitsCredentialsPathAndQuery(t *testing.T) {
	rawURL := "https://user:password@example.com:8443/mcp?state=secret-state#fragment"

	if got := platformMCPLogHost(rawURL); got != "example.com:8443" {
		t.Fatalf("platformMCPLogHost() = %q, want %q", got, "example.com:8443")
	}
}
