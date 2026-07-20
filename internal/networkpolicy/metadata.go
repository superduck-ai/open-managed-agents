package networkpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
)

const mcpAllowedHostsMetadataKey = "mcp_allowed_hosts"

// ErrMalformedWorkMetadata marks malformed network-policy fields in
// Environment Work metadata. Callers must fail closed when MCP egress is on.
var ErrMalformedWorkMetadata = errors.New("malformed environment work network metadata")

type workNetworkMetadataSchema struct {
	MCPAllowedHosts []string `json:"mcp_allowed_hosts"`
}

// PatchWorkMetadataMCPAllowedHosts updates the typed network-policy field while
// preserving metadata owned by other features.
func PatchWorkMetadataMCPAllowedHosts(raw json.RawMessage, hosts []string) (json.RawMessage, error) {
	fields, err := parseWorkMetadataEnvelope(raw)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeMetadataHosts(hosts)
	if err != nil {
		return nil, err
	}
	if normalized == nil {
		normalized = []string{}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedWorkMetadata, err)
	}
	fields[mcpAllowedHostsMetadataKey] = encoded
	return json.Marshal(fields)
}

// ParseWorkMetadataMCPAllowedHosts reads the typed network-policy field. A
// missing field means no MCP hosts; an explicit null or wrong shape is invalid.
func ParseWorkMetadataMCPAllowedHosts(raw json.RawMessage) ([]string, error) {
	fields, err := parseWorkMetadataEnvelope(raw)
	if err != nil {
		return nil, err
	}
	value, ok := fields[mcpAllowedHostsMetadataKey]
	if !ok {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, ErrMalformedWorkMetadata
	}
	var schema workNetworkMetadataSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedWorkMetadata, err)
	}
	return normalizeMetadataHosts(schema.MCPAllowedHosts)
}

func parseWorkMetadataEnvelope(raw json.RawMessage) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fields, nil
	}
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedWorkMetadata, err)
	}
	return fields, nil
}

func normalizeMetadataHosts(hosts []string) ([]string, error) {
	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		value, err := NormalizeHost(host)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid MCP host: %v", ErrMalformedWorkMetadata, err)
		}
		normalized = append(normalized, value)
	}
	return collections.UniqueTrimmedStrings(normalized), nil
}
