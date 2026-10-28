package llmproviders

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestOrganizationProviderSecretBindingUsesStableWorkspace(t *testing.T) {
	binding := SecretBinding(db.LLMProvider{OrganizationUUID: "org", ExternalID: "llmprov_org"})
	if binding.WorkspaceUUID != db.OrganizationLLMProviderSecretWorkspace || binding.VaultExternalID != secretScope {
		t.Fatalf("organization binding = %#v", binding)
	}
	if binding.CredentialExternalID != "llmprov_org" || binding.OrganizationUUID != "org" {
		t.Fatalf("organization binding identity = %#v", binding)
	}
	workspace := SecretBinding(db.LLMProvider{OrganizationUUID: "org", WorkspaceUUID: "workspace", ExternalID: "llmprov_ws"})
	if workspace.WorkspaceUUID != "workspace" {
		t.Fatalf("workspace binding = %#v", workspace)
	}
}

func TestValidateBaseURL(t *testing.T) {
	valid, err := ValidateBaseURL("https://dashscope.example.com/apps/anthropic/")
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
		" https://example.com",
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

func TestEndpointAcceptsCompleteRequestURLs(t *testing.T) {
	for _, base := range []string{
		"https://example.com/prefix/v1/messages",
		"https://example.com/prefix/v1/chat/completions/",
		"https://example.com/prefix/v1",
	} {
		for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/models"} {
			got, err := Endpoint(base, path, "")
			if err != nil || got != "https://example.com/prefix"+path {
				t.Fatalf("Endpoint(%q, %q) = %q, %v", base, path, got, err)
			}
		}
	}
}
