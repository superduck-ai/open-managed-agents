package networkpolicy

import (
	"sync"
	"testing"
)

// ---- 失败场景 ----

func TestValidateAllowedHostRejectsInvalidEntries(t *testing.T) {
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
		" example.com",
		"example.com ",
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
}

func TestHostMatcherZeroValueDoesNotMatch(t *testing.T) {
	if (hostMatcher{}).match("api.example.com", "443") {
		t.Fatal("zero hostMatcher must not match")
	}
}

func TestHostMatcherRequiresConfiguredPort(t *testing.T) {
	entries, err := parseConfigAllowedHosts([]string{"api.example.com:8443", "*.example.org:9443", "public.example.net"})
	if err != nil {
		t.Fatalf("parseConfigAllowedHosts() error = %v", err)
	}
	matcher := newHostMatcher(entries)
	if matcher.match("api.example.com", "443") || matcher.match("service.example.org", "443") {
		t.Fatal("port-specific entries must not match a different port")
	}
	if !matcher.match("api.example.com", "8443") || !matcher.match("service.example.org", "9443") {
		t.Fatal("port-specific entries must match their configured port")
	}
	if !matcher.match("public.example.net", "8080") {
		t.Fatal("host-only entries must match any valid target port")
	}
}

func TestHostMatcherSeparatesExactAndWildcardRulesAtSameNode(t *testing.T) {
	entries, err := parseConfigAllowedHosts([]string{"example.com:443", "*.example.com:8443"})
	if err != nil {
		t.Fatalf("parseConfigAllowedHosts() error = %v", err)
	}
	matcher := newHostMatcher(entries)
	if matcher.match("example.com", "8443") || matcher.match("api.example.com", "443") {
		t.Fatal("exact and wildcard rules must not borrow each other's ports")
	}
	if !matcher.match("example.com", "443") || !matcher.match("api.example.com", "8443") {
		t.Fatal("exact and wildcard rules must match their own ports")
	}
}

// ---- 成功场景 ----

func TestValidateAllowedHostAcceptsCanonicalizableEntries(t *testing.T) {
	for _, host := range []string{
		"example.com",
		"*.example.com",
		"例子.测试",
		"*.例子.测试",
		"example.com:8443",
		"2606:4700:4700::1111",
		"[2606:4700:4700::1111]:443",
	} {
		if err := ValidateAllowedHost(host); err != nil {
			t.Fatalf("host %q: unexpected error %v", host, err)
		}
	}
}

func TestHostMatcherSupportsConcurrentReads(t *testing.T) {
	entries, err := parseConfigAllowedHosts([]string{"api.example.com", "*.googleapis.com"})
	if err != nil {
		t.Fatalf("parseConfigAllowedHosts() error = %v", err)
	}
	matcher := newHostMatcher(entries)

	const workers = 32
	results := make(chan bool, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- matcher.match("api.example.com", "443") &&
				matcher.match("storage.googleapis.com", "443") &&
				!matcher.match("example.org", "443")
		}()
	}
	group.Wait()
	close(results)

	for matched := range results {
		if !matched {
			t.Fatal("concurrent hostMatcher read returned an unexpected result")
		}
	}
}
