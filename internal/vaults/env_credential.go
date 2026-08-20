package vaults

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	opaquePlaceholderPrefix = "oma_ph_"
	maxSecretNameLength     = 255
)

type credentialInjectionLocation struct {
	Header bool `json:"header"`
	Body   bool `json:"body"`
}

// claudeRuntimeModelEnvironmentKeys mirrors the model env names written by
// environments.buildEnvironmentManagerV0Payload so vault credentials cannot
// reserve the same names. Kept here to avoid an import cycle.
var claudeRuntimeModelEnvironmentKeys = []string{
	"ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
}

type credentialInjectionLocationInput struct {
	Header *bool `json:"header"`
	Body   *bool `json:"body"`
}

// decodeEnvironmentCredentialAuth decodes one environment_variable public auth
// and rejects legacy credentials missing persisted Opaque Placeholder or
// Injection Location (archive and recreate).
func decodeEnvironmentCredentialAuth(authJSON []byte) (*environmentVariableCredentialAuth, error) {
	auth, err := decodeCredentialAuth(authJSON)
	if err != nil {
		return nil, fmt.Errorf("environment variable credential auth is invalid: %w", err)
	}
	return requireReadyEnvironmentAuth(auth.value)
}

func requireReadyEnvironmentAuth(value credentialAuthVariant) (*environmentVariableCredentialAuth, error) {
	env, ok := value.(*environmentVariableCredentialAuth)
	if !ok || env == nil {
		return nil, errors.New("credential is not environment_variable")
	}
	if strings.TrimSpace(env.Placeholder) == "" {
		return nil, errors.New("environment variable credential is missing placeholder; archive and recreate")
	}
	if !env.InjectionLocation.Header && !env.InjectionLocation.Body {
		return nil, errors.New("environment variable credential is missing injection_location; archive and recreate")
	}
	return env, nil
}

type environmentCredential struct {
	row   db.VaultCredential
	value *environmentVariableCredentialAuth
}

// uniqueEnvironmentCredentials walks credentials in Vault Attachment Order.
// Reserved names are omitted from the result but still set hasEnv (MITM is
// required if any env credential is attached). First secret_name wins.
func uniqueEnvironmentCredentials(credentials []db.VaultCredential) (bool, []environmentCredential, error) {
	hasEnv := false
	seen := make(map[string]struct{})
	bound := make([]environmentCredential, 0)
	for i := range credentials {
		if credentialAuthType(credentials[i].AuthType) != credentialAuthTypeEnvironmentVariable {
			continue
		}
		hasEnv = true
		value, err := decodeEnvironmentCredentialAuth(credentials[i].Auth)
		if err != nil {
			return false, nil, err
		}
		if PlatformReservedSecretName(value.SecretName) {
			continue
		}
		if _, exists := seen[value.SecretName]; exists {
			continue
		}
		seen[value.SecretName] = struct{}{}
		bound = append(bound, environmentCredential{row: credentials[i], value: value})
	}
	return hasEnv, bound, nil
}

// parseSecretName returns the secret_name to persist. Callers do not trim,
// match POSIX identifiers, or consult the reserved-name set themselves.
func parseSecretName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", errors.New("auth.secret_name must be non-empty")
	}
	if len(name) > maxSecretNameLength {
		return "", errors.New("auth.secret_name must be at most 255 characters")
	}
	if !posixEnvironmentName(name) {
		return "", errors.New("auth.secret_name must be a POSIX environment variable name")
	}
	if PlatformReservedSecretName(name) {
		return "", fmt.Errorf("auth.secret_name %q is reserved", name)
	}
	return name, nil
}

func posixEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isLetter := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if c == '_' || isLetter {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func generateOpaquePlaceholder() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate opaque placeholder: %w", err)
	}
	return opaquePlaceholderPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func applyInjectionLocation(base credentialInjectionLocation, raw json.RawMessage) (credentialInjectionLocation, error) {
	if len(raw) == 0 {
		return base, nil
	}
	if isJSONNull(raw) {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must be omitted instead of null")
	}
	var input credentialInjectionLocationInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must be an object")
	}
	next := base
	if input.Header != nil {
		next.Header = *input.Header
	}
	if input.Body != nil {
		next.Body = *input.Body
	}
	if !next.Header && !next.Body {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must enable header or body")
	}
	return next, nil
}

// PlatformReservedSecretName reports whether secretName is owned by the
// platform (Managed Agent / Code Session startup) and cannot be used as an
// Environment Variable Credential secret_name.
func PlatformReservedSecretName(secretName string) bool {
	_, reserved := platformReservedSecretNames[strings.ToUpper(secretName)]
	return reserved
}

// platformReservedSecretNames cannot be used as Environment Variable Credential
// secret_name values; they are owned by Managed Agent / Code Session startup.
var platformReservedSecretNames = func() map[string]struct{} {
	names := []string{
		"CLAUDE_CODE_REMOTE",
		"CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2",
		"CLAUDE_CODE_USE_CCR_V2",
		"CLAUDE_CODE_WORKER_EPOCH",
		"CLAUDE_CODE_SESSION_ACCESS_TOKEN",
		"CCR_UPSTREAM_PROXY_ENABLED",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
		"CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL",
		"CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_TRACES_EXPORTER",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_HEADERS",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE",
		"ENABLE_BETA_TRACING_DETAILED",
		"BETA_TRACING_ENDPOINT",
	}
	out := make(map[string]struct{}, len(names)+len(claudeRuntimeModelEnvironmentKeys))
	for _, name := range names {
		out[strings.ToUpper(name)] = struct{}{}
	}
	for _, name := range claudeRuntimeModelEnvironmentKeys {
		out[strings.ToUpper(name)] = struct{}{}
	}
	return out
}()
