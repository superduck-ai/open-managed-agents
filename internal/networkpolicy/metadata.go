package networkpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
)

const mcpAllowedHostsMetadataKey = "mcp_allowed_hosts"

// ErrMalformedWorkMetadata 表示 Environment Work metadata 中的网络策略字段
// 格式错误；MCP egress 开启时调用方必须 fail closed。
var ErrMalformedWorkMetadata = errors.New("malformed environment work network metadata")

type mcpAllowedHostsSchema []string

// PatchWorkMetadataMCPAllowedHosts 更新类型化的网络策略字段，同时保留其他功能
// 所拥有的 metadata。
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
	encoded, err := json.Marshal(mcpAllowedHostsSchema(normalized))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedWorkMetadata, err)
	}
	fields[mcpAllowedHostsMetadataKey] = encoded
	return json.Marshal(fields)
}

// ParseWorkMetadataMCPAllowedHosts 读取类型化的网络策略字段。字段缺失表示没有
// MCP hosts；显式 null 或错误的数据结构均视为非法。
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
	var schema mcpAllowedHostsSchema
	if err := json.Unmarshal(value, &schema); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedWorkMetadata, err)
	}
	return normalizeMetadataHosts(schema)
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
