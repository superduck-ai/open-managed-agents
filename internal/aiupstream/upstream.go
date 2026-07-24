package aiupstream

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func NewHTTPClient(transport http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func Endpoint(baseURL string, resourcePath string, rawQuery string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", errors.New("AI gateway base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("AI gateway base URL must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("AI gateway base URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("AI gateway base URL must not contain credentials, a query, or a fragment")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "anthropic.com" || strings.HasSuffix(hostname, ".anthropic.com") {
		return "", errors.New("public Anthropic upstreams are not allowed")
	}

	endpoint, err := url.JoinPath(parsed.String(), strings.TrimLeft(resourcePath, "/"))
	if err != nil {
		return "", fmt.Errorf("build AI gateway endpoint: %w", err)
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse AI gateway endpoint: %w", err)
	}
	endpointURL.RawQuery = rawQuery
	return endpointURL.String(), nil
}

func ValidateDeployment(baseURL string, apiKey string) error {
	if _, err := Endpoint(baseURL, "v1/models", ""); err != nil {
		return err
	}
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("AI gateway API key is required")
	}
	return nil
}
