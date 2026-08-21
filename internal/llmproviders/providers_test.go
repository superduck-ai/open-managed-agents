package llmproviders

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestValidateBaseURL(t *testing.T) {
	valid, err := ValidateBaseURL(" https://dashscope.example.com/apps/anthropic/ ")
	if err != nil || valid != "https://dashscope.example.com/apps/anthropic" {
		t.Fatalf("ValidateBaseURL() = (%q, %v)", valid, err)
	}

	for _, rawURL := range []string{
		"http://127.0.0.1:11434",
		"https://localhost:8443",
		"https://10.0.0.1",
		"https://example.com:8443",
	} {
		if _, err := ValidateBaseURL(rawURL); err != nil {
			t.Fatalf("ValidateBaseURL(%q) error = %v, want nil", rawURL, err)
		}
	}

	for _, rawURL := range []string{
		"ftp://example.com",
		"https://user:pass@example.com",
		"https://example.com?token=secret",
		"https://example.com#fragment",
		"example.com",
	} {
		if _, err := ValidateBaseURL(rawURL); err == nil {
			t.Fatalf("ValidateBaseURL(%q) succeeded, want error", rawURL)
		}
	}
}

func TestEndpointPreservesProviderPathAndRequestModelIsNotInURL(t *testing.T) {
	endpoint, err := Endpoint("https://dashscope.example.com/apps/anthropic", "/v1/messages", "beta=true")
	if err != nil {
		t.Fatalf("Endpoint() error: %v", err)
	}
	if endpoint != "https://dashscope.example.com/apps/anthropic/v1/messages?beta=true" {
		t.Fatalf("Endpoint() = %q", endpoint)
	}
}

func TestProviderHTTPClientDoesNotFollowRedirects(t *testing.T) {
	client := NewHTTPClient(time.Second)
	request, err := http.NewRequest(http.MethodGet, "https://other.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v, want http.ErrUseLastResponse", err)
	}
}
