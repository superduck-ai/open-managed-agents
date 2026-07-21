package networkpolicy

import (
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// ---- 失败场景 ----

func TestPackageManagerCatalogExcludesVCSHosts(t *testing.T) {
	for _, host := range PackageManagerHosts() {
		if host == "github.com" || host == "gitlab.com" || host == "bitbucket.org" {
			t.Fatalf("catalog must not include VCS host %q", host)
		}
	}
}

// ---- 成功场景 ----

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
	fixture := rawPolicyFixture{Config: limitedConfig(t, `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":true}`)}
	decision := authorizeHTTPSFixture(t, fixture, target)
	if !decision.Allow || decision.Reason != ReasonPackageManagerHost {
		t.Fatalf("live redirect target %q is not authorized: %+v", target, decision)
	}
}
