package networkpolicy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"
)

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
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "example.com:443")
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

func TestAuthorizeHTTPSFailsClosedOnUnknownNetworkingType(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"bogus"}`)}
	decision := AuthorizeHTTPS(subject, "example.com:443")
	if decision.Allow {
		t.Fatal("unknown networking type must fail closed")
	}
	if decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyUnavailable, decision.Reason)
	}
}

func TestAuthorizeHTTPSFailsClosedOnMalformedConfig(t *testing.T) {
	subject := Subject{Config: json.RawMessage(`{"type":"cloud","networking":{`)}
	decision := AuthorizeHTTPS(subject, "example.com:443")
	if decision.Allow {
		t.Fatal("malformed config must fail closed")
	}
	if decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyUnavailable, decision.Reason)
	}
}

func TestAuthorizeHTTPSFailsClosedOnInvalidAllowedHost(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["bad/path"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "api.example.com:443")
	if decision.Allow {
		t.Fatal("invalid allowed_hosts policy must fail closed")
	}
	if decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyUnavailable, decision.Reason)
	}
}

func TestAuthorizeHTTPSDoesNotPartiallyAcceptMalformedAllowlist(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["bad/path","api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "api.example.com:443")
	if decision.Allow {
		t.Fatal("one invalid entry must invalidate the complete policy")
	}
	if decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyUnavailable, decision.Reason)
	}
}

func TestParseConfigClassifiesInvalidAllowedHostAsMalformed(t *testing.T) {
	_, err := ParseConfig(limitedConfig(t, `{"type":"limited","allowed_hosts":["bad/path"]}`))
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestAuthorizeHTTPSRejectsMalformedTarget(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	for _, target := range []string{"example.com", "example.com:abc", ":443", "example.com:443:443"} {
		decision := AuthorizeHTTPS(subject, target)
		if decision.Allow {
			t.Fatalf("target %q must be rejected", target)
		}
		if decision.Reason != ReasonInvalidTarget {
			t.Fatalf("target %q: expected reason %q, got %q", target, ReasonInvalidTarget, decision.Reason)
		}
	}
}

func TestAuthorizeHTTPSRejectsNon443Target(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := AuthorizeHTTPS(subject, "example.com:8443")
	if decision.Allow {
		t.Fatal("non-443 target must be rejected")
	}
	if decision.Reason != ReasonInvalidTarget {
		t.Fatalf("expected reason %q, got %q", ReasonInvalidTarget, decision.Reason)
	}
}

func TestAuthorizeHTTPSWildcardDoesNotMatchApex(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "example.com:443")
	if decision.Allow {
		t.Fatal("wildcard entry must not match apex")
	}
	if decision.Reason != ReasonHostNotAllowed {
		t.Fatalf("expected reason %q, got %q", ReasonHostNotAllowed, decision.Reason)
	}
}

func TestAuthorizeHTTPSWildcardDoesNotMatchSibling(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "other.com:443")
	if decision.Allow {
		t.Fatal("wildcard entry must not match unrelated host")
	}
}

func TestAuthorizeHTTPEntryWithNon443PortIsInert(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["example.com:8443"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "example.com:443")
	if decision.Allow {
		t.Fatal("entry with non-443 port must be inert for the 443-only proxy")
	}
}

func TestAuthorizeHTTPSDeniesMCPHostWhenSwitchOff(t *testing.T) {
	subject := Subject{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "https://mcp.example.com/sse"),
	}
	decision := AuthorizeHTTPS(subject, "mcp.example.com:443")
	if decision.Allow {
		t.Fatal("MCP host must be denied when allow_mcp_servers is false")
	}
	if decision.Reason != ReasonHostNotAllowed {
		t.Fatalf("expected reason %q, got %q", ReasonHostNotAllowed, decision.Reason)
	}
}

func TestAuthorizeHTTPSFailsClosedOnMalformedAgentSnapshotWhenMCPAllowed(t *testing.T) {
	subject := Subject{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true,"allow_package_managers":false}`),
		AgentSnapshot: json.RawMessage(`{"mcp_servers":[`),
	}
	decision := AuthorizeHTTPS(subject, "api.example.com:443")
	if decision.Allow {
		t.Fatal("malformed AgentSnapshot must invalidate the complete policy")
	}
	if decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyUnavailable, decision.Reason)
	}
}

func TestAuthorizeHTTPSFailsClosedOnMalformedMCPURL(t *testing.T) {
	subject := Subject{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "://bad"),
	}
	decision := AuthorizeHTTPS(subject, "api.example.com:443")
	if decision.Allow {
		t.Fatal("malformed MCP URL must invalidate the complete policy")
	}
	if decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyUnavailable, decision.Reason)
	}
}

func TestAuthorizeHTTPSDeniesMCPHostNotInSnapshot(t *testing.T) {
	subject := Subject{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "https://mcp.example.com/sse"),
	}
	decision := AuthorizeHTTPS(subject, "other-mcp.example.com:443")
	if decision.Allow {
		t.Fatal("host not referenced by the session snapshot must be denied")
	}
}

func TestAuthorizeHTTPSDeniesPackageHostWhenSwitchOff(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "pypi.org:443")
	if decision.Allow {
		t.Fatal("package registry host must be denied when allow_package_managers is false")
	}
}

func TestAuthorizeHTTPSDeniesNonCatalogHostWhenPackageSwitchOn(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	decision := AuthorizeHTTPS(subject, "evil-packages.example.com:443")
	if decision.Allow {
		t.Fatal("non-catalog host must be denied")
	}
}

func TestPackageManagerCatalogExcludesVCSHosts(t *testing.T) {
	for _, host := range PackageManagerHosts() {
		if host == "github.com" || host == "gitlab.com" || host == "bitbucket.org" {
			t.Fatalf("catalog must not include VCS host %q", host)
		}
	}
}

func TestParseConfigMalformedJSON(t *testing.T) {
	if _, err := ParseConfig(json.RawMessage(`{"type":`)); !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestParseConfigUnknownNetworkingType(t *testing.T) {
	if _, err := ParseConfig(limitedConfig(t, `{"type":"bogus"}`)); !errors.Is(err, ErrUnknownNetworkingType) {
		t.Fatalf("expected ErrUnknownNetworkingType, got %v", err)
	}
}

func TestParseConfigEmptyNetworkingTypeFailsClosed(t *testing.T) {
	// networking 对象存在但 type 为空：写入路径会归一化为 unrestricted，存储中不应出现；
	// 一旦出现必须 fail closed，不能静默降级为 unrestricted。
	if _, err := ParseConfig(limitedConfig(t, `{"type":"","allowed_hosts":["api.example.com"]}`)); !errors.Is(err, ErrUnknownNetworkingType) {
		t.Fatalf("expected ErrUnknownNetworkingType, got %v", err)
	}
	decision := AuthorizeHTTPS(Subject{Config: limitedConfig(t, `{"type":""}`)}, "api.example.com:443")
	if decision.Allow || decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("empty networking type must fail closed, got %+v", decision)
	}
}

func TestMCPAllowedHostsRejectsMalformedSnapshot(t *testing.T) {
	if _, err := MCPAllowedHosts(json.RawMessage(`{"mcp_servers":[`)); !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	}
}

func TestMCPAllowedHostsRejectsMalformedURL(t *testing.T) {
	if _, err := MCPAllowedHosts(snapshotWithMCP(t, "://bad")); !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	}
}

func TestMCPAllowedHostsRejectsEmptySnapshot(t *testing.T) {
	if _, err := MCPAllowedHosts(nil); !errors.Is(err, ErrMalformedAgentSnapshot) {
		t.Fatalf("expected ErrMalformedAgentSnapshot, got %v", err)
	}
}

func TestMCPAllowedHostsRejectsMalformedRemoteServerContracts(t *testing.T) {
	for _, snapshot := range []json.RawMessage{
		json.RawMessage(`{"mcp_servers":[{"url":"https://evil.example/mcp"}]}`),
		json.RawMessage(`{"mcp_servers":[{"type":"stdio","url":"https://evil.example/mcp"}]}`),
		json.RawMessage(`{"mcp_servers":[{"type":"url","url":"ftp://evil.example/mcp"}]}`),
		json.RawMessage(`{"mcp_servers":[{"type":"url","url":"//evil.example/mcp"}]}`),
	} {
		hosts, err := MCPAllowedHosts(snapshot)
		if !errors.Is(err, ErrMalformedAgentSnapshot) {
			t.Fatalf("snapshot %s: expected ErrMalformedAgentSnapshot, got hosts=%v err=%v", snapshot, hosts, err)
		}
	}
}

// ---- 成功场景 ----

func TestAuthorizeHTTPSAllowsExplicitHost(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "api.example.com:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
	if decision.Reason != ReasonExplicitHost {
		t.Fatalf("expected reason %q, got %q", ReasonExplicitHost, decision.Reason)
	}
}

func TestAuthorizeHTTPSWildcardMatchesAnyDepthSubdomain(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	for _, target := range []string{"a.example.com:443", "a.b.example.com:443"} {
		decision := AuthorizeHTTPS(subject, target)
		if !decision.Allow {
			t.Fatalf("target %q: expected allow, got deny with reason %q", target, decision.Reason)
		}
		if decision.Reason != ReasonExplicitHost {
			t.Fatalf("target %q: expected reason %q, got %q", target, ReasonExplicitHost, decision.Reason)
		}
	}
}

func TestAuthorizeHTTPSNormalizesTargetBeforeMatching(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "API.Example.COM.:443")
	if !decision.Allow {
		t.Fatalf("expected allow after normalization, got deny with reason %q", decision.Reason)
	}
	if decision.Host != "api.example.com" {
		t.Fatalf("expected normalized host api.example.com, got %q", decision.Host)
	}
}

func TestAuthorizeHTTPSAllowsPublicIPv6WhenUnrestricted(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := AuthorizeHTTPS(subject, "[2606:4700:4700::1111]:443")
	if !decision.Allow || decision.Reason != ReasonUnrestricted {
		t.Fatalf("expected unrestricted IPv6 allow, got %+v", decision)
	}
	if decision.Host != "2606:4700:4700::1111" {
		t.Fatalf("normalized host = %q, want 2606:4700:4700::1111", decision.Host)
	}
}

func TestAuthorizeHTTPSAllowsExplicitIPv6Host(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["2606:4700:4700::1111"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "[2606:4700:4700::1111]:443")
	if !decision.Allow || decision.Reason != ReasonExplicitHost {
		t.Fatalf("expected explicit IPv6 allow, got %+v", decision)
	}
}

func TestAuthorizeHTTPSCanonicalizesIPv4MappedIPv6(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := AuthorizeHTTPS(subject, "[::ffff:192.0.2.1]:443")
	if !decision.Allow || decision.Host != "192.0.2.1" {
		t.Fatalf("expected mapped IPv6 to normalize as IPv4, got %+v", decision)
	}
}

func TestAuthorizeHTTPSAllowsCommonHyphenatedEdgeHostname(t *testing.T) {
	const host = "r3---sn-apo3qvuoxuxbt-j5pe.googlevideo.com"
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["`+host+`"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, host+":443")
	if !decision.Allow || decision.Reason != ReasonExplicitHost {
		t.Fatalf("expected common edge hostname allow, got %+v", decision)
	}
}

func TestAuthorizeHTTPSAllowsEntryWith443Port(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":["api.example.com:443"],"allow_mcp_servers":false,"allow_package_managers":false}`)}
	decision := AuthorizeHTTPS(subject, "api.example.com:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
}

func TestAuthorizeHTTPSAllowsMCPHostWhenSwitchOn(t *testing.T) {
	subject := Subject{
		Config:        limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true,"allow_package_managers":false}`),
		AgentSnapshot: snapshotWithMCP(t, "https://mcp.example.com/sse"),
	}
	decision := AuthorizeHTTPS(subject, "MCP.example.com:443")
	if !decision.Allow {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
	if decision.Reason != ReasonMCPHost {
		t.Fatalf("expected reason %q, got %q", ReasonMCPHost, decision.Reason)
	}
}

func TestAuthorizeHTTPSAllowsPackageHostsWhenSwitchOn(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
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
		decision := AuthorizeHTTPS(subject, target)
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
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	for _, target := range []string{"proxy.golang.org:443", "storage.googleapis.com:443"} {
		decision := AuthorizeHTTPS(subject, target)
		if !decision.Allow || decision.Reason != ReasonPackageManagerHost {
			t.Fatalf("target %q: expected package redirect chain allow, got %+v", target, decision)
		}
	}
}

func TestPackageManagerHostsReturnsIndependentCatalogCopy(t *testing.T) {
	hosts := PackageManagerHosts()
	if len(hosts) == 0 {
		t.Fatal("package manager catalog is empty")
	}
	originalFirst := hosts[0]
	hosts[0] = "mutated.example"
	if next := PackageManagerHosts(); next[0] != originalFirst {
		t.Fatalf("catalog was mutated through returned slice: %v", next)
	}
}

func TestLiveLargeGoModuleProxyRedirectHostIsAuthorized(t *testing.T) {
	if os.Getenv("TEST_GO_MODULE_PROXY_REDIRECT") != "1" {
		t.Skip("set TEST_GO_MODULE_PROXY_REDIRECT=1 to verify the live Go module proxy redirect")
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Head("https://proxy.golang.org/github.com/aws/aws-sdk-go/@v/v1.55.8.zip")
	if err != nil {
		t.Fatalf("request large Go module zip: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("large Go module status = %d, want redirect", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil || location.Hostname() == "" {
		t.Fatalf("parse Go module redirect location %q: %v", response.Header.Get("Location"), err)
	}
	target := location.Hostname() + ":443"
	subject := Subject{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	decision := AuthorizeHTTPS(subject, target)
	if !decision.Allow || decision.Reason != ReasonPackageManagerHost {
		t.Fatalf("live redirect target %q is not authorized: %+v", target, decision)
	}
}

func TestAuthorizeHTTPSAllowsUnrestricted(t *testing.T) {
	subject := Subject{Config: limitedConfig(t, `{"type":"unrestricted"}`)}
	decision := AuthorizeHTTPS(subject, "anything.example.org:443")
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
		decision := AuthorizeHTTPS(Subject{Config: raw}, "example.com:443")
		if !decision.Allow || decision.Reason != ReasonUnrestricted {
			t.Fatalf("config %s: expected unrestricted allow, got %+v", raw, decision)
		}
	}
}

// ---- ParseConfig ----

func TestParseConfigLimitedFields(t *testing.T) {
	config, err := ParseConfig(limitedConfig(t, `{"type":"limited","allowed_hosts":["a.com","b.com"],"allow_mcp_servers":true,"allow_package_managers":true}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Type != "limited" || !config.AllowMCPServers || !config.AllowPackageManagers {
		t.Fatalf("unexpected config: %+v", config)
	}
	if len(config.AllowedHosts) != 2 || config.AllowedHosts[0] != "a.com" {
		t.Fatalf("unexpected hosts: %v", config.AllowedHosts)
	}
}

// ---- MCPAllowedHosts ----

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
	if len(hosts) != 2 || hosts[0] != "mcp.example.com" || hosts[1] != "other.example.org" {
		t.Fatalf("unexpected hosts: %v", hosts)
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

// ---- ValidateAllowedHost ----

func TestValidateAllowedHost(t *testing.T) {
	for _, host := range []string{
		"https://example.com",
		"example.com/path",
		"",
		".",
		"..",
		"example..com",
		"-example.com",
		"example-.com",
		"example.com:0",
		"example.com:65536",
		"exa mple.com",
		"a-b.c_d.com",
	} {
		if err := ValidateAllowedHost(host); err == nil {
			t.Fatalf("host %q: expected error", host)
		}
	}
	long := make([]byte, 254)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateAllowedHost(string(long)); err == nil {
		t.Fatal("expected error for host longer than 253 characters")
	}
	for _, host := range []string{
		"example.com",
		"*.example.com",
		"example.com:8443",
		"2606:4700:4700::1111",
		"[2606:4700:4700::1111]:443",
	} {
		if err := ValidateAllowedHost(host); err != nil {
			t.Fatalf("host %q: unexpected error %v", host, err)
		}
	}
}
