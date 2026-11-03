package config

import (
	"fmt"
	"net/url"
	"strings"
)

func OAuthCredentialTargetURL(bindings []OAuthServiceBindingConfig, serviceURL string) string {
	for _, binding := range bindings {
		if binding.ServiceURL == serviceURL {
			return binding.CredentialTargetURL
		}
	}
	return serviceURL
}

func validateOAuthServiceBindings(bindings []OAuthServiceBindingConfig) error {
	seen := make(map[string]bool, len(bindings))
	for i := range bindings {
		binding := &bindings[i]
		binding.ServiceURL = strings.TrimSpace(binding.ServiceURL)
		binding.CredentialTargetURL = strings.TrimSpace(binding.CredentialTargetURL)
		for _, value := range []string{binding.ServiceURL, binding.CredentialTargetURL} {
			parsed, err := url.Parse(value)
			if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("vault.oauth_service_bindings[%d] requires absolute HTTP URLs without userinfo, query or fragment", i)
			}
		}
		if seen[binding.ServiceURL] {
			return fmt.Errorf("vault.oauth_service_bindings[%d].service_url is duplicated", i)
		}
		seen[binding.ServiceURL] = true
	}
	return nil
}
