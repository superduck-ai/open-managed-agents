package sessions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func TestSessionErrorPayloadRejectsInvalidKnownFields(t *testing.T) {
	for _, details := range []string{
		`null`, `[]`, `"private-runtime-error"`, `{}`,
		`{"type":"unknown_error","message":null,"retry_status":{"type":"retrying"}}`,
		`{"type":"unknown_error","message":3,"retry_status":{"type":"retrying"}}`,
		`{"TYPE":"unknown_error","message":"private-runtime-error","retry_status":{"type":"retrying"}}`,
		`{"type":"unknown_error","MESSAGE":"private-runtime-error","retry_status":{"type":"retrying"}}`,
		`{"type":"future_error","message":"private-runtime-error","retry_status":{"type":"retrying"}}`,
		`{"type":"unknown_error","message":"private-runtime-error","retry_status":"retrying"}`,
		`{"type":"unknown_error","message":"private-runtime-error","retry_status":null}`,
		`{"type":"unknown_error","message":"private-runtime-error","retry_status":{"type":"future_retry"}}`,
		`{"type":"unknown_error","message":"private-runtime-error","retry_status":{"TYPE":"retrying"}}`,
		`{"type":"mcp_connection_failed_error","message":"private-runtime-error","retry_status":{"type":"retrying"}}`,
		`{"type":"mcp_authentication_failed_error","message":"private-runtime-error","retry_status":{"type":"terminal"},"mcp_server_name":null}`,
		`{"type":"credential_host_unreachable_error","message":"private-runtime-error","retry_status":{"type":"exhausted"},"credential_id":"id"}`,
		`{"type":"credential_host_unreachable_error","message":"private-runtime-error","retry_status":{"type":"exhausted"},"credential_id":12,"vault_id":"id"}`,
	} {
		err := validateSessionErrorEvent(json.RawMessage(`{"type":"session.error","error":` + details + `}`))
		if !errors.Is(err, codesessions.ErrProtocol) || strings.Contains(err.Error(), "private-runtime-error") {
			t.Fatalf("invalid payload returned unsafe or unexpected error: %v", err)
		}
	}
}

func TestSessionErrorPayloadPreservesCanonicalVariants(t *testing.T) {
	for _, errorType := range []string{
		"unknown_error", "model_overloaded_error", "model_rate_limited_error", "model_request_failed_error",
		"billing_error", "mcp_connection_failed_error", "mcp_authentication_failed_error", "credential_host_unreachable_error",
	} {
		for _, retryType := range []string{"retrying", "exhausted", "terminal"} {
			t.Run(errorType+"/"+retryType, func(t *testing.T) {
				raw := json.RawMessage(`{"type":"session.error","error":{"type":"` + errorType + `","message":"","retry_status":{"type":"` + retryType + `","TYPE":"future_retry"},"TYPE":"future_error","mcp_server_name":"","credential_id":"","vault_id":"","future_counter":9007199254740993}}`)
				before := string(raw)
				if err := validateSessionErrorEvent(raw); err != nil {
					t.Fatal(err)
				}
				if string(raw) != before {
					t.Fatal("validation changed the canonical payload")
				}
			})
		}
	}
}
