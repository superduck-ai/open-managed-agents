package networkpolicy

import (
	"encoding/json"
	"errors"
	"testing"
)

// rawPolicyFixture keeps raw fixtures at the test boundary while production callers
// parse database JSON into Policy before authorization.
type rawPolicyFixture struct {
	Config        json.RawMessage
	AgentSnapshot json.RawMessage
}

func authorizeHTTPSFixture(t *testing.T, fixture rawPolicyFixture, target string) Decision {
	t.Helper()
	policy, err := ParsePolicy(fixture.Config, fixture.AgentSnapshot)
	if err != nil {
		t.Fatalf("ParsePolicy() error = %v", err)
	}
	return policy.AuthorizeHTTPS(target)
}

func limitedConfig(t *testing.T, networking string) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{"type":"cloud","networking":` + networking + `}`)
}

func snapshotWithMCP(t *testing.T, urls ...string) json.RawMessage {
	t.Helper()
	servers := ``
	for i, u := range urls {
		if i > 0 {
			servers += `,`
		}
		servers += `{"type":"http","url":` + quoteJSON(u) + `}`
	}
	return json.RawMessage(`{"mcp_servers":[` + servers + `]}`)
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---- 失败场景 ----

func TestAuthorizeHTTPSDeniesLimitedEmptyAllowlist(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "example.com:443")
	if decision.Allow {
		t.Fatalf("expected deny, got allow with reason %q", decision.Reason)
	}
	if decision.Reason != ReasonHostNotAllowed {
		t.Fatalf("expected reason %q, got %q", ReasonHostNotAllowed, decision.Reason)
	}
	if decision.Host != "example.com" {
		t.Fatalf("expected normalized host example.com, got %q", decision.Host)
	}
}

func TestAuthorizeHTTPSZeroPolicyFailsClosed(t *testing.T) {
	decision := (Policy{}).AuthorizeHTTPS("example.com:443")
	if decision.Allow || decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("zero Policy decision = %+v, want policy_unavailable denial", decision)
	}
}

func TestParsePolicyRejectsUnknownNetworkingType(t *testing.T) {
	_, err := ParsePolicy(limitedConfig(t, `{"type":"bogus"}`), nil)
	if !errors.Is(err, ErrUnknownNetworkingType) {
		t.Fatalf("expected ErrUnknownNetworkingType, got %v", err)
	}
}

func TestParsePolicyRejectsMalformedConfig(t *testing.T) {
	_, err := ParsePolicy(json.RawMessage(`{"type":"cloud","networking":{`), nil)
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestParsePolicyRejectsInvalidAllowedHost(t *testing.T) {
	_, err := ParsePolicy(limitedConfig(t, `{"type":"limited","allowed_hosts":["bad/path"],"allow_mcp_servers":false,"allow_package_managers":false}`), nil)
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestParsePolicyDoesNotPartiallyAcceptMalformedAllowlist(t *testing.T) {
	_, err := ParsePolicy(limitedConfig(t, `{"type":"limited","allowed_hosts":["bad/path","api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`), nil)
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestAuthorizeHTTPSRejectsMalformedTarget(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	for _, target := range []string{"example.com", "example.com:abc", ":443", "example.com:443:443"} {
		decision := authorizeHTTPSFixture(t, fixture, target)
		if decision.Allow {
			t.Fatalf("target %q must be rejected", target)
		}
		if decision.Reason != ReasonInvalidTarget {
			t.Fatalf("target %q: expected reason %q, got %q", target, ReasonInvalidTarget, decision.Reason)
		}
	}
}

func TestAuthorizeHTTPSRejectsNon443Target(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := authorizeHTTPSFixture(t, fixture, "example.com:8443")
	if decision.Allow {
		t.Fatal("non-443 target must be rejected")
	}
	if decision.Reason != ReasonInvalidTarget {
		t.Fatalf("expected reason %q, got %q", ReasonInvalidTarget, decision.Reason)
	}
}

func TestAuthorizeHTTPSWildcardDoesNotMatchApex(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "example.com:443")
	if decision.Allow {
		t.Fatal("wildcard entry must not match apex")
	}
	if decision.Reason != ReasonHostNotAllowed {
		t.Fatalf("expected reason %q, got %q", ReasonHostNotAllowed, decision.Reason)
	}
}

func TestAuthorizeHTTPSWildcardDoesNotMatchSibling(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "other.com:443")
	if decision.Allow {
		t.Fatal("wildcard entry must not match unrelated host")
	}
}

func TestAuthorizeHTTPEntryWithNon443PortIsInert(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["example.com:8443"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "example.com:443")
	if decision.Allow {
		t.Fatal("entry with non-443 port must be inert for the 443-only proxy")
	}
}

func TestAuthorizeHTTPSDeniesMCPHostWhenSwitchOff(t *testing.T) {
	fixture := rawPolicyFixture{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "https://mcp.example.com/sse"),
	}
	decision := authorizeHTTPSFixture(t, fixture, "mcp.example.com:443")
	if decision.Allow {
		t.Fatal("MCP host must be denied when allow_mcp_servers is false")
	}
	if decision.Reason != ReasonHostNotAllowed {
		t.Fatalf("expected reason %q, got %q", ReasonHostNotAllowed, decision.Reason)
	}
}

func TestParsePolicyRejectsMalformedAgentSnapshotWhenMCPAllowed(t *testing.T) {
	_, err := ParsePolicy(
		limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true,"allow_package_managers":false}`),
		json.RawMessage(`{"mcp_servers":[`),
	)
	if !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	}
}

func TestParsePolicyRejectsMalformedMCPURL(t *testing.T) {
	_, err := ParsePolicy(
		limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true,"allow_package_managers":false}`),
		snapshotWithMCP(t, "://bad"),
	)
	if !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	}
}

func TestAuthorizeHTTPSDeniesMCPHostNotInSnapshot(t *testing.T) {
	fixture := rawPolicyFixture{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "https://mcp.example.com/sse"),
	}
	decision := authorizeHTTPSFixture(t, fixture, "other-mcp.example.com:443")
	if decision.Allow {
		t.Fatal("host not referenced by the session snapshot must be denied")
	}
}

func TestAuthorizeHTTPSDeniesPackageHostWhenSwitchOff(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "pypi.org:443")
	if decision.Allow {
		t.Fatal("package registry host must be denied when allow_package_managers is false")
	}
}

func TestAuthorizeHTTPSDeniesNonCatalogHostWhenPackageSwitchOn(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	decision := authorizeHTTPSFixture(t, fixture, "evil-packages.example.com:443")
	if decision.Allow {
		t.Fatal("non-catalog host must be denied")
	}
}

// ---- 成功场景 ----

func TestAuthorizeHTTPSAllowsExplicitHost(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "api.example.com:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
	if decision.Reason != ReasonExplicitHost {
		t.Fatalf("expected reason %q, got %q", ReasonExplicitHost, decision.Reason)
	}
}

func TestAuthorizeHTTPSWildcardMatchesAnyDepthSubdomain(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	for _, target := range []string{"a.example.com:443", "a.b.example.com:443"} {
		decision := authorizeHTTPSFixture(t, fixture, target)
		if !decision.Allow {
			t.Fatalf("target %q: expected allow, got deny with reason %q", target, decision.Reason)
		}
		if decision.Reason != ReasonExplicitHost {
			t.Fatalf("target %q: expected reason %q, got %q", target, ReasonExplicitHost, decision.Reason)
		}
	}
}

func TestAuthorizeHTTPSNormalizesTargetBeforeMatching(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "API.Example.COM.:443")
	if !decision.Allow {
		t.Fatalf("expected allow after normalization, got deny with reason %q", decision.Reason)
	}
	if decision.Host != "api.example.com" {
		t.Fatalf("expected normalized host api.example.com, got %q", decision.Host)
	}
}

func TestAuthorizeHTTPSAllowsPublicIPv6WhenUnrestricted(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := authorizeHTTPSFixture(t, fixture, "[2606:4700:4700::1111]:443")
	if !decision.Allow || decision.Reason != ReasonUnrestricted {
		t.Fatalf("expected unrestricted IPv6 allow, got %+v", decision)
	}
	if decision.Host != "2606:4700:4700::1111" {
		t.Fatalf("normalized host = %q, want 2606:4700:4700::1111", decision.Host)
	}
}

func TestAuthorizeHTTPSAllowsExplicitIPv6Host(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["2606:4700:4700::1111"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "[2606:4700:4700::1111]:443")
	if !decision.Allow || decision.Reason != ReasonExplicitHost {
		t.Fatalf("expected explicit IPv6 allow, got %+v", decision)
	}
}

func TestAuthorizeHTTPSCanonicalizesIPv4MappedIPv6(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := authorizeHTTPSFixture(t, fixture, "[::ffff:192.0.2.1]:443")
	if !decision.Allow || decision.Host != "192.0.2.1" {
		t.Fatalf("expected mapped IPv6 to normalize as IPv4, got %+v", decision)
	}
}

func TestAuthorizeHTTPSAllowsCommonHyphenatedEdgeHostname(t *testing.T) {
	const host = "r3---sn-apo3qvuoxuxbt-j5pe.googlevideo.com"
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["`+host+`"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, host+":443")
	if !decision.Allow || decision.Reason != ReasonExplicitHost {
		t.Fatalf("expected common edge hostname allow, got %+v", decision)
	}
}

func TestAuthorizeHTTPSAllowsEntryWith443Port(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com:443"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := authorizeHTTPSFixture(t, fixture, "api.example.com:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
}

func TestAuthorizeHTTPSAllowsMCPHostWhenSwitchOn(t *testing.T) {
	fixture := rawPolicyFixture{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "https://mcp.example.com/sse"),
	}
	decision := authorizeHTTPSFixture(t, fixture, "MCP.example.com:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
	if decision.Reason != ReasonMCPHost {
		t.Fatalf("expected reason %q, got %q", ReasonMCPHost, decision.Reason)
	}
}

func TestAuthorizeHTTPSAllowsPackageHostsWhenSwitchOn(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	for _, target := range []string{
		"pypi.org:443",
		"files.pythonhosted.org:443",
		"registry.npmjs.org:443",
		"registry.npmmirror.com:443",
		"proxy.golang.org:443",
		"goproxy.cn:443",
		"crates.io:443",
		"rubygems.org:443",
		"mirrors.tuna.tsinghua.edu.cn:443",
		"archive.ubuntu.com:443",
	} {
		decision := authorizeHTTPSFixture(t, fixture, target)
		if !decision.Allow {
			t.Fatalf("target %q: expected allow, got deny with reason %q", target, decision.Reason)
		}
		if decision.Reason != ReasonPackageManagerHost {
			t.Fatalf("target %q: expected reason %q, got %q", target, ReasonPackageManagerHost, decision.Reason)
		}
	}
}

func TestAuthorizeHTTPSAllowsLargeGoModuleProxyRedirectChain(t *testing.T) {
	// github.com/aws/aws-sdk-go@v1.55.8 的 module zip 会从
	// proxy.golang.org 重定向到 storage.googleapis.com。
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	for _, target := range []string{"proxy.golang.org:443", "storage.googleapis.com:443"} {
		decision := authorizeHTTPSFixture(t, fixture, target)
		if !decision.Allow || decision.Reason != ReasonPackageManagerHost {
			t.Fatalf("target %q: expected package redirect chain allow, got %+v", target, decision)
		}
	}
}

func TestAuthorizeHTTPSAllowsUnrestricted(t *testing.T) {
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := authorizeHTTPSFixture(t, fixture, "anything.example.org:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
	if decision.Reason != ReasonUnrestricted {
		t.Fatalf("expected reason %q, got %q", ReasonUnrestricted, decision.Reason)
	}
}

func TestAuthorizeHTTPSUnrestrictedWhenNetworkingAbsent(t *testing.T) {
	for _, raw := range []json.RawMessage{
		nil,
		json.RawMessage(`{}`),
		json.RawMessage(`{"type":"cloud"}`),
		json.RawMessage(`{"type":"local","networking":{"type":"limited","allowed_hosts":[]}}`),
	} {
		decision := authorizeHTTPSFixture(t, rawPolicyFixture{Config: raw}, "example.com:443")
		if !decision.Allow || decision.Reason != ReasonUnrestricted {
			t.Fatalf("config %s: expected unrestricted allow, got %+v", raw, decision)
		}
	}
}
