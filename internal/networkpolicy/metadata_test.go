package networkpolicy

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// ---- 失败场景 ----

func TestParseWorkMetadataMCPAllowedHostsRejectsMalformedKnownField(t *testing.T) {
	for _, metadata := range []json.RawMessage{
		json.RawMessage(`{"mcp_allowed_hosts":null}`),
		json.RawMessage(`{"mcp_allowed_hosts":"mcp.example.com"}`),
		json.RawMessage(`{"mcp_allowed_hosts":["mcp.example.com",42]}`),
	} {
		if _, err := ParseWorkMetadataMCPAllowedHosts(metadata); !errors.Is(err, ErrMalformedWorkMetadata) {
			t.Fatalf("metadata %s: expected ErrMalformedWorkMetadata, got %v", metadata, err)
		}
	}
}

func TestParseWorkMetadataMCPAllowedHostsUsesExactFieldValue(t *testing.T) {
	metadata := json.RawMessage(`{"mcp_allowed_hosts":"invalid","MCP_ALLOWED_HOSTS":["shadow.example.com"]}`)
	if _, err := ParseWorkMetadataMCPAllowedHosts(metadata); !errors.Is(err, ErrMalformedWorkMetadata) {
		t.Fatalf("expected exact invalid field to fail closed, got %v", err)
	}
}

func TestPatchWorkMetadataMCPAllowedHostsRejectsInvalidHost(t *testing.T) {
	if _, err := PatchWorkMetadataMCPAllowedHosts(nil, []string{"bad_host"}); !errors.Is(err, ErrMalformedWorkMetadata) {
		t.Fatalf("expected ErrMalformedWorkMetadata, got %v", err)
	}
}

// ---- 成功场景 ----

func TestParseWorkMetadataMCPAllowedHostsTreatsMissingFieldAsEmpty(t *testing.T) {
	for _, metadata := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{}`), json.RawMessage(`{"other":"value"}`)} {
		hosts, err := ParseWorkMetadataMCPAllowedHosts(metadata)
		if err != nil {
			t.Fatalf("metadata %s: unexpected error: %v", metadata, err)
		}
		if len(hosts) != 0 {
			t.Fatalf("metadata %s: hosts = %v, want empty", metadata, hosts)
		}
	}
}

func TestPatchWorkMetadataMCPAllowedHostsPreservesOtherFields(t *testing.T) {
	patched, err := PatchWorkMetadataMCPAllowedHosts(
		json.RawMessage(`{"managed_agent_skills_mount":{"volume_name":"skills"},"mcp_allowed_hosts":["stale.example"]}`),
		[]string{"MCP.Example.com", "mcp.example.com"},
	)
	if err != nil {
		t.Fatalf("PatchWorkMetadataMCPAllowedHosts() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patched, &fields); err != nil {
		t.Fatalf("decode patched metadata: %v", err)
	}
	if _, ok := fields["managed_agent_skills_mount"]; !ok {
		t.Fatalf("unrelated metadata was removed: %s", patched)
	}
	hosts, err := ParseWorkMetadataMCPAllowedHosts(patched)
	if err != nil {
		t.Fatalf("ParseWorkMetadataMCPAllowedHosts() error = %v", err)
	}
	if !slices.Equal(hosts, []string{"mcp.example.com"}) {
		t.Fatalf("hosts = %v, want [mcp.example.com]", hosts)
	}
}
