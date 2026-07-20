package networkpolicy

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// ---- 失败场景 ----

func TestMCPAllowedHostsRejectsMalformedSnapshot(t *testing.T) {
	if _, err := MCPAllowedHosts(json.RawMessage(`{"mcp_servers":[`)); !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	}
}

func TestMCPAllowedHostsRejectsMalformedURL(t *testing.T) {
	if _, err := MCPAllowedHosts(snapshotWithMCP(t, "://bad")); !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	} else if !strings.Contains(err.Error(), `server type "http" has an invalid URL`) {
		t.Fatalf("error = %q, want invalid URL detail", err)
	}
}

func TestMCPAllowedHostsRejectsEmptyOrNullSnapshot(t *testing.T) {
	for _, snapshot := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{"mcp_servers":null}`)} {
		if _, err := MCPAllowedHosts(snapshot); !errors.Is(err, ErrMalformedAgentSnapshot) {
			t.Fatalf("snapshot %s: expected ErrMalformedAgentSnapshot, got %v", snapshot, err)
		}
	}
}

func TestMCPAllowedHostsRejectsMalformedRemoteServerContracts(t *testing.T) {
	tests := []struct {
		name     string
		snapshot json.RawMessage
		want     string
	}{
		{name: "missing type", snapshot: json.RawMessage(`{"mcp_servers":[{"url":"https://evil.example/mcp"}]}`), want: `unsupported MCP server type ""`},
		{name: "stdio URL", snapshot: json.RawMessage(`{"mcp_servers":[{"type":"stdio","url":"https://evil.example/mcp"}]}`), want: `server type "stdio" must not include a URL`},
		{name: "missing URL", snapshot: json.RawMessage(`{"mcp_servers":[{"type":"url"}]}`), want: `server type "url" requires a URL`},
		{name: "unsupported scheme", snapshot: json.RawMessage(`{"mcp_servers":[{"type":"url","url":"ftp://evil.example/mcp"}]}`), want: "must use http or https"},
		{name: "relative URL", snapshot: json.RawMessage(`{"mcp_servers":[{"type":"url","url":"//evil.example/mcp"}]}`), want: "must be absolute"},
		{name: "missing host", snapshot: json.RawMessage(`{"mcp_servers":[{"type":"url","url":"https:///mcp"}]}`), want: "must include a host"},
		{name: "invalid host", snapshot: json.RawMessage(`{"mcp_servers":[{"type":"url","url":"https://bad_host/mcp"}]}`), want: "has an invalid host"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hosts, err := MCPAllowedHosts(test.snapshot)
			if !errors.Is(err, ErrMalformedAgentSnapshot) {
				t.Fatalf("expected ErrMalformedAgentSnapshot, got hosts=%v err=%v", hosts, err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want detail %q", err, test.want)
			}
		})
	}
}

// ---- 成功场景 ----

func TestMCPAllowedHostsExtractsDedupesAndNormalizes(t *testing.T) {
	snapshot := json.RawMessage(`{"mcp_servers":[
		{"type":"http","url":"https://MCP.example.com/api"},
		{"type":"sse","url":"https://mcp.example.com/sse"},
		{"type":"stdio","command":"npx"},
		{"type":"http","url":"https://other.example.org"}
	]}`)
	hosts, err := MCPAllowedHosts(snapshot)
	if err != nil {
		t.Fatalf("extract MCP hosts: %v", err)
	}
	want := []string{"mcp.example.com", "other.example.org"}
	if !slices.Equal(hosts, want) {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
}

func TestMCPAllowedHostsAcceptsCanonicalAndTransportRemoteServerTypes(t *testing.T) {
	snapshot := json.RawMessage(`{"mcp_servers":[
		{"type":"url","url":"https://canonical.example/mcp"},
		{"type":"http","url":"http://streamable.example/mcp"},
		{"type":"sse","url":"https://events.example/sse"},
		{"type":"stdio","command":"npx"}
	]}`)
	hosts, err := MCPAllowedHosts(snapshot)
	if err != nil {
		t.Fatalf("extract MCP hosts: %v", err)
	}
	want := []string{"canonical.example", "streamable.example", "events.example"}
	if !slices.Equal(hosts, want) {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
}
