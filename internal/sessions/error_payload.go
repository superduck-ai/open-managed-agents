package sessions

import "encoding/json"

type sessionErrorDetails struct {
	Type      string
	Message   string
	RetryType string
}

// Validate the worker's canonical error without normalizing the stored JSON or
// inferring a state transition from it. History remains forward-compatible.
func validateSessionErrorEvent(raw json.RawMessage) error {
	var event, details, retry map[string]json.RawMessage
	if json.Unmarshal(raw, &event) != nil || rawSessionEventType(raw) != "session.error" ||
		json.Unmarshal(event["error"], &details) != nil || details == nil ||
		json.Unmarshal(details["retry_status"], &retry) != nil || retry == nil {
		return invalidSessionErrorPayload()
	}
	var payload sessionErrorDetails
	if !readSessionErrorString(details["type"], &payload.Type) ||
		!readSessionErrorString(details["message"], &payload.Message) ||
		!readSessionErrorString(retry["type"], &payload.RetryType) {
		return invalidSessionErrorPayload()
	}
	switch payload.RetryType {
	case "retrying", "exhausted", "terminal":
	default:
		return invalidSessionErrorPayload()
	}
	var required []string
	switch payload.Type {
	case "unknown_error", "model_overloaded_error", "model_rate_limited_error", "model_request_failed_error", "billing_error":
	case "mcp_connection_failed_error", "mcp_authentication_failed_error":
		required = []string{"mcp_server_name"}
	case "credential_host_unreachable_error":
		required = []string{"credential_id", "vault_id"}
	default:
		return invalidSessionErrorPayload()
	}
	for _, field := range required {
		var value string
		if !readSessionErrorString(details[field], &value) {
			return invalidSessionErrorPayload()
		}
	}
	return nil
}

// Required strings may be empty, but cannot be missing, null, or another type.
// Exact map keys prevent case-insensitive aliases from overriding known fields.
func readSessionErrorString(raw json.RawMessage, target *string) bool {
	var value *string
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	*target = *value
	return true
}
