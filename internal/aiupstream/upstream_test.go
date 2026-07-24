package aiupstream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEndpointRejectsMissingAndPublicAnthropicUpstreams(t *testing.T) {
	t.Parallel()
	for _, baseURL := range []string{
		"",
		"gateway.internal",
		"https://api.anthropic.com",
		"https://API.ANTHROPIC.COM/tenant",
		"https://edge.anthropic.com",
	} {
		if _, err := Endpoint(baseURL, "v1/messages", ""); err == nil {
			t.Fatalf("Endpoint(%q) error = nil, want rejection", baseURL)
		}
	}
}

func TestEndpointPreservesGatewayPrefixAndQuery(t *testing.T) {
	t.Parallel()
	got, err := Endpoint("https://gateway.internal/tenant/", "v1/messages", "beta=true")
	if err != nil {
		t.Fatalf("Endpoint() error = %v", err)
	}
	if got != "https://gateway.internal/tenant/v1/messages?beta=true" {
		t.Fatalf("Endpoint() = %q", got)
	}
}

func TestValidateDeploymentRequiresGatewayCredentials(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		baseURL string
		apiKey  string
		want    string
	}{
		{name: "missing URL", apiKey: "secret", want: "base URL"},
		{name: "missing key", baseURL: "https://gateway.internal", want: "API key"},
		{name: "public Anthropic", baseURL: "https://api.anthropic.com", apiKey: "secret", want: "public Anthropic"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDeployment(testCase.baseURL, testCase.apiKey)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("ValidateDeployment() error = %v, want %q", err, testCase.want)
			}
		})
	}

	if err := ValidateDeployment("https://gateway.internal", "secret"); err != nil {
		t.Fatalf("ValidateDeployment() error = %v", err)
	}
}

func TestHTTPClientDoesNotForwardGatewayCredentialsAcrossRedirects(t *testing.T) {
	t.Parallel()
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/v1/messages")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer gateway.Close()

	request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(`{"model":"provider/model"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("X-Api-Key", "gateway-secret")
	response, err := NewHTTPClient(nil, 0).Do(request)
	if err != nil {
		t.Fatalf("request gateway: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want redirect response", response.StatusCode)
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target requests = %d, want zero", redirectedRequests.Load())
	}
}
