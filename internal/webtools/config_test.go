package webtools

import (
	"strings"
	"testing"
)

func TestWebConfigValidation(t *testing.T) {
	for _, raw := range []string{
		`{"name":"web_fetch","allowed_domains":[],"blocked_domains":["example.com"]}`,
		`{"name":"web_fetch","allowed_domains":[]}`,
		`{"name":"web_fetch","allowed_domains":["example.com","EXAMPLE.COM"]}`,
		`{"name":"web_fetch","allowed_domains":["*.example.com"]}`,
		`{"name":"web_fetch","allowed_domains":["https://example.com"]}`,
		`{"name":"web_fetch","allowed_domains":["127.0.0.1"]}`,
		`{"name":"web_fetch","allowed_domains":["example.com/path"]}`,
		`{"name":"web_fetch","max_content_tokens":0}`,
		`{"name":"web_search","max_content_tokens":20}`,
		`{"name":"web_search","user_location":{"type":"approximate"}}`,
		`{"name":"web_search","user_location":{"type":"precise","country":"US"}}`,
		`{"name":"web_search","user_location":{"type":"approximate","country":"US"}}`,
	} {
		if _, err := ParseToolConfig([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"name":"web_search","allowed_domains":["example.com/blog"]}`, `{"name":"web_fetch","allowed_domains":null}`, `{"name":"web_fetch","max_content_tokens":20}`, `{"name":"web_fetch","blocked_domains":["ads.example.com"]}`} {
		if _, err := ParseToolConfig([]byte(raw)); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestLayeredPoliciesOnlyNarrowAccess(t *testing.T) {
	p := network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com","*.example.com"]}}`)
	p.Tools = []ToolConfig{{AllowedDomains: []string{"example.com"}}, {BlockedDomains: []string{"ads.example.com"}}, {AllowedDomains: []string{"docs.example.com"}}}
	for _, tc := range []struct {
		url   string
		allow bool
	}{{"https://docs.example.com", true}, {"https://nested.docs.example.com", true}, {"https://example.com", false}, {"https://ads.example.com", false}, {"https://example.net", false}} {
		target, err := publicURL(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if p.Allows(target) != tc.allow {
			t.Errorf("%s", tc.url)
		}
	}
	raw := []byte(`{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"web_fetch","allowed_domains":["docs.example.com"]}]}]}`)
	if err := ValidateSession([]byte(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`), raw); err == nil {
		t.Fatal("conflicting session accepted")
	}
	if err := ValidateSession([]byte(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["*.example.com"]}}`), raw); err != nil {
		t.Fatal(err)
	}
}

func TestTranslatedPrivateAddressAndHTML(t *testing.T) {
	title, content := renderHTML("<title>Title</title><style>secret</style><h2>Heading</h2><p>Some &amp; text</p><pre>x\n y</pre>")
	if title != "Title" || strings.Contains(content, "secret") || !strings.Contains(content, "## Heading") || !strings.Contains(content, "Some & text") {
		t.Fatalf("%s %s", title, content)
	}
}
