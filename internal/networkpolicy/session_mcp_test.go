package networkpolicy

import (
	"encoding/json"
	"testing"
)

func TestSessionMCPHostValidation(t *testing.T) {
	for _, tc := range []struct {
		name, networking, endpoint string
		allowed                    bool
	}{
		{"default denied", `{"type":"limited","allowed_hosts":[]}`, "https://api.example.com/mcp", false},
		{"package manager does not grant MCP", `{"type":"limited","allowed_hosts":[],"allow_package_managers":true}`, "https://pypi.org/mcp", false},
		{"port mismatch", `{"type":"limited","allowed_hosts":["api.example.com:8443"]}`, "https://api.example.com/mcp", false},
		{"wildcard excludes apex", `{"type":"limited","allowed_hosts":["*.example.com"]}`, "https://example.com/mcp", false},
		{"lookalike host", `{"type":"limited","allowed_hosts":["example.com"]}`, "https://example.com.evil.test/mcp", false},
		{"normalized host", `{"type":"limited","allowed_hosts":["api.example.com"]}`, "https://API.Example.COM./mcp", true},
		{"wildcard subdomain", `{"type":"limited","allowed_hosts":["*.example.com"]}`, "https://api.example.com/mcp", true},
		{"explicit port", `{"type":"limited","allowed_hosts":["api.example.com:8443"]}`, "https://api.example.com:8443/mcp", true},
		{"HTTP default port", `{"type":"limited","allowed_hosts":["api.example.com:80"]}`, "http://api.example.com/mcp", true},
		{"MCP exception", `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}`, "https://api.example.com/mcp", true},
		{"unrestricted", `{"type":"unrestricted"}`, "https://api.example.com/mcp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{"name": "peer", "type": "url", "url": tc.endpoint}}})
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateSessionMCPHosts(json.RawMessage(`{"type":"cloud","networking":`+tc.networking+`}`), snapshot)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
	for _, tc := range []struct{ config, snapshot string }{
		{`{"type":"cloud","networking":{"type":"unknown"}}`, `{}`},
		{`{"type":"cloud","networking":{"type":"limited"}}`, `{"mcp_servers":null}`},
		{`{"type":"cloud","networking":{"type":"limited"}}`, `{"mcp_servers":[{"type":"url","url":"not-url"}]}`},
	} {
		if ValidateSessionMCPHosts(json.RawMessage(tc.config), json.RawMessage(tc.snapshot)) == nil {
			t.Fatal("malformed input accepted", tc)
		}
	}
	if err := ValidateSessionMCPHosts(json.RawMessage(`{"type":"cloud","networking":{"type":"limited"}}`), json.RawMessage(`{"mcp_servers":[]}`)); err != nil {
		t.Fatal("empty MCP declaration rejected", err)
	}
}

func TestSessionMCPHostsIncludesEnabledPredefinedAgents(t *testing.T) {
	blocked := json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false}}`)
	allowed := json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["child.example.com"],"allow_mcp_servers":false}}`)
	snapshot := json.RawMessage(`{"mcp_servers":[],"multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"type":"agent","id":"expert","version":1,"mcp_servers":[{"name":"peer","type":"url","url":"https://child.example.com/mcp"}]}]},"workflows":{"type":"disabled"}}}`)
	if err := ValidateSessionMCPHosts(blocked, snapshot); err == nil {
		t.Fatal("Session admitted a predefined expert whose MCP host was excluded")
	}
	if err := ValidateSessionMCPHosts(allowed, snapshot); err != nil {
		t.Fatal("explicitly allowed expert host rejected", err)
	}
	exception := json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}}`)
	if err := ValidateSessionMCPHosts(exception, snapshot); err != nil {
		t.Fatal("explicit MCP exception rejected", err)
	}
}

func TestSessionMCPHostsValidatesEveryReachableExpertScope(t *testing.T) {
	config := json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["allowed.example.com"],"allow_mcp_servers":false}}`)
	for _, tc := range []struct {
		name, snapshot string
		allowed        bool
	}{
		{"second expert excluded", `{"mcp_servers":[],"multiagent":{"subagents":{"type":"enabled","predefined_agents":[{"mcp_servers":[{"name":"peer","type":"url","url":"https://allowed.example.com/mcp"}]},{"mcp_servers":[{"name":"peer","type":"url","url":"https://blocked.example.com/mcp"}]}]}}}`, false},
		{"nested expert excluded", `{"mcp_servers":[],"multiagent":{"subagents":{"type":"enabled","predefined_agents":[{"mcp_servers":[],"multiagent":{"subagents":{"type":"enabled","predefined_agents":[{"mcp_servers":[{"name":"peer","type":"url","url":"https://blocked.example.com/mcp"}]}]}}}]}}}`, false},
		{"malformed child server", `{"mcp_servers":[],"multiagent":{"subagents":{"type":"enabled","predefined_agents":[{"mcp_servers":[{"name":"peer","type":"url","url":"not-url"}]}]}}}`, false},
		{"null child server list", `{"mcp_servers":[],"multiagent":{"subagents":{"type":"enabled","predefined_agents":[{"mcp_servers":null}]}}}`, false},
		{"same name separate valid scopes", `{"mcp_servers":[{"name":"peer","type":"url","url":"https://allowed.example.com/parent"}],"multiagent":{"subagents":{"type":"enabled","predefined_agents":[{"mcp_servers":[{"name":"peer","type":"url","url":"https://allowed.example.com/child"}]}]}}}`, true},
		{"disabled roster not reachable", `{"mcp_servers":[],"multiagent":{"subagents":{"type":"disabled","predefined_agents":[{"mcp_servers":[{"name":"peer","type":"url","url":"https://blocked.example.com/mcp"}]}]}}}`, true},
		{"workflow roster uses same host rule", `{"mcp_servers":[],"multiagent":{"workflows":{"type":"enabled","predefined_agents":[{"mcp_servers":[{"name":"peer","type":"url","url":"https://blocked.example.com/mcp"}]}]}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSessionMCPHosts(config, json.RawMessage(tc.snapshot))
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
