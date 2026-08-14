package vaults

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const opaquePlaceholderPrefix = "oma_ph_"

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
	value, ok := auth.value.(*environmentVariableCredentialAuth)
	if !ok || value == nil {
		return nil, errors.New("credential is not environment_variable")
	}
	if strings.TrimSpace(value.Placeholder) == "" {
		return nil, errors.New("environment variable credential is missing placeholder; archive and recreate")
	}
	if !value.InjectionLocation.Header && !value.InjectionLocation.Body {
		return nil, errors.New("environment variable credential is missing injection_location; archive and recreate")
	}
	return value, nil
}

func generateOpaquePlaceholder() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate opaque placeholder: %w", err)
	}
	return opaquePlaceholderPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func normalizeInjectionLocationForCreate(raw json.RawMessage) (credentialInjectionLocation, error) {
	if len(raw) == 0 {
		return credentialInjectionLocation{Header: true, Body: false}, nil
	}
	if isJSONNull(raw) {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must be omitted instead of null")
	}
	var input credentialInjectionLocationInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must be an object")
	}
	// CMA Console default: header only.
	location := credentialInjectionLocation{Header: true, Body: false}
	if input.Header != nil {
		location.Header = *input.Header
	}
	if input.Body != nil {
		location.Body = *input.Body
	}
	if !location.Header && !location.Body {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must enable header or body")
	}
	return location, nil
}

func mergeInjectionLocationForUpdate(current credentialInjectionLocation, raw json.RawMessage) (credentialInjectionLocation, error) {
	if len(raw) == 0 {
		return current, nil
	}
	if isJSONNull(raw) {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must be omitted instead of null")
	}
	var input credentialInjectionLocationInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return credentialInjectionLocation{}, errors.New("auth.injection_location must be an object")
	}
	next := current
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
	_, reserved := platformReservedSecretNames[secretName]
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
		out[name] = struct{}{}
	}
	for _, name := range claudeRuntimeModelEnvironmentKeys {
		out[name] = struct{}{}
	}
	return out
}()

func validateSecretNameNotReserved(secretName string) error {
	if PlatformReservedSecretName(secretName) {
		return fmt.Errorf("auth.secret_name %q is reserved", secretName)
	}
	return nil
}
