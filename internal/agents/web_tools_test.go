package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeWebToolsPreservesRestrictions(t *testing.T) {
	raw := json.RawMessage(`[{"type":"agent_toolset_20260401","configs":[{"name":"web_fetch","allowed_domains":["docs.example.com"],"max_content_tokens":42}]}]`)
	out, err := normalizeTools(raw, json.RawMessage(`[]`))
	if err != nil || !strings.Contains(string(out), `"max_content_tokens":42`) || !strings.Contains(string(out), `"allowed_domains":["docs.example.com"]`) {
		t.Fatalf("%s %v", out, err)
	}
	for _, config := range []string{`{"name":"web_fetch","allowed_domains":[]}`, `{"name":"web_fetch","allowed_domains":["example.com"],"blocked_domains":["example.net"]}`, `{"name":"web_fetch","max_content_tokens":-1}`} {
		if _, err := normalizeTools(json.RawMessage(`[{"type":"agent_toolset_20260401","configs":[`+config+`]}]`), json.RawMessage(`[]`)); err == nil {
			t.Fatal("invalid web config accepted")
		}
	}
}
