package mcpcatalogs

import (
	"strings"
	"testing"
)

func TestNormalizeEndpointRejectsUnsupportedEndpoints(t *testing.T) {
	tests := []string{
		"",
		"arthurs-MacBook-Pro-2.local:39090/mcp",
		"ftp://example.com/mcp",
		"https://user:password@example.com/mcp",
		"https://example.com/mcp?token=secret",
		"https://example.com/mcp#fragment",
	}
	for _, endpoint := range tests {
		if _, err := NormalizeEndpoint(endpoint); err == nil {
			t.Fatalf("NormalizeEndpoint(%q) unexpectedly succeeded", endpoint)
		}
	}
}

func TestNormalizeEndpointRejectsURLTooLongForCatalogIdentity(t *testing.T) {
	endpoint := "https://example.com/" + strings.Repeat("a", maxEndpointURLBytes)
	if _, err := NormalizeEndpoint(endpoint); err == nil {
		t.Fatal("NormalizeEndpoint unexpectedly accepted an oversized endpoint")
	}
}

func TestNormalizeEndpointCanonicalizesHTTPURL(t *testing.T) {
	got, err := NormalizeEndpoint(" HTTPS://Example.COM.:443/mcp ")
	if err != nil {
		t.Fatalf("NormalizeEndpoint returned error: %v", err)
	}
	if want := "https://example.com/mcp"; got != want {
		t.Fatalf("NormalizeEndpoint = %q, want %q", got, want)
	}
}

func TestNormalizeEndpointAcceptsCatalogIdentityAtLengthLimit(t *testing.T) {
	prefix := "https://example.com/"
	endpoint := prefix + strings.Repeat("a", maxEndpointURLBytes-len(prefix))
	got, err := NormalizeEndpoint(endpoint)
	if err != nil {
		t.Fatalf("NormalizeEndpoint returned error at length limit: %v", err)
	}
	if len(got) != maxEndpointURLBytes {
		t.Fatalf("normalized endpoint length = %d, want %d", len(got), maxEndpointURLBytes)
	}
}
