package llmproviders

import (
	"net/http"
)

const (
	Messages        = "messages"
	ChatCompletions = "chat_completions"
)

func ValidBackend(backend string) bool {
	return backend == Messages || backend == ChatCompletions
}

func (u Upstream) RequireBackend(backend string) error {
	actual := u.APIBackend
	if actual == "" {
		actual = Messages
	}
	if actual != backend {
		return ErrBackendMismatch
	}
	return nil
}

func (u Upstream) ApplyHeaders(headers http.Header) {
	headers.Del("Anthropic-Version")
	headers.Del("Anthropic-Beta")
	headers.Del("X-Api-Key")
	headers.Set("Authorization", "Bearer "+u.APIKey)
	if u.APIBackend != ChatCompletions {
		ApplyAPIKey(headers, u.APIKey)
		headers.Set("Anthropic-Version", anthropicVersion)
	}
}
