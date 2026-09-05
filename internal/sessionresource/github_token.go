package sessionresource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var ErrGitHubTokenStorage = errors.New("GitHub resource credential storage failed")

type githubTokenEnvelope struct {
	Envelope secrets.Envelope `json:"envelope"`
}

// ParseGitHubToken accepts a write-only HTTP input, without persisting it.
func ParseGitHubToken(raw json.RawMessage) (string, error) {
	var token string
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &token); err != nil {
			return "", errors.New("authorization_token must be a string or null")
		}
	}
	if len(token) > 8192 || strings.ContainsFunc(token, unicode.IsSpace) || strings.ContainsFunc(token, unicode.IsControl) {
		return "", errors.New("authorization_token must not contain whitespace or control characters and must be at most 8192 bytes")
	}
	return token, nil
}

// SealGitHubToken stores an authenticated envelope, or null for anonymous access.
func SealGitHubToken(ctx context.Context, service *secrets.Service, binding secrets.ResourceBinding, token string) (json.RawMessage, error) {
	if token == "" {
		return json.RawMessage(`null`), nil
	}
	if service == nil {
		return nil, ErrGitHubTokenStorage
	}
	plaintext := []byte(token)
	defer clear(plaintext)
	envelope, err := service.SealResource(ctx, binding, plaintext)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitHubTokenStorage, err)
	}
	return json.Marshal(githubTokenEnvelope{Envelope: envelope})
}

// OpenGitHubToken rejects historical plaintext rows. Re-submit the token through
// Session resource rotation or a Deployment resource replacement to repair them.
func OpenGitHubToken(ctx context.Context, service *secrets.Service, binding secrets.ResourceBinding, raw json.RawMessage) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", nil
	}
	if service == nil {
		return "", ErrGitHubTokenStorage
	}
	var envelope githubTokenEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Envelope.FormatVersion == 0 {
		return "", errors.New("GitHub resource token must be re-submitted before use")
	}
	plaintext, err := service.OpenResource(ctx, binding, envelope.Envelope)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGitHubTokenStorage, err)
	}
	defer clear(plaintext)
	return string(plaintext), nil
}
