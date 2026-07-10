package mcpcatalogs

import "testing"

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

func TestNormalizeEndpointCanonicalizesHTTPURL(t *testing.T) {
	got, err := NormalizeEndpoint(" HTTPS://Example.COM.:443/mcp ")
	if err != nil {
		t.Fatalf("NormalizeEndpoint returned error: %v", err)
	}
	if want := "https://example.com/mcp"; got != want {
		t.Fatalf("NormalizeEndpoint = %q, want %q", got, want)
	}
}

func TestEndpointKeyIsTransportBoundAndStable(t *testing.T) {
	first := EndpointKey("secret", "https://example.com/mcp")
	second := EndpointKey("secret", "https://example.com/mcp")
	otherSecret := EndpointKey("other", "https://example.com/mcp")
	if first != second {
		t.Fatalf("EndpointKey is not stable: %q != %q", first, second)
	}
	if first == otherSecret {
		t.Fatal("EndpointKey must depend on the configured HMAC secret")
	}
	if len(first) != len("mcpe_")+sha256HexLength {
		t.Fatalf("EndpointKey length = %d", len(first))
	}
}

const sha256HexLength = 64
