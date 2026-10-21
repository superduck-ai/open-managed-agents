package mcpnames

import (
	"strings"
	"testing"
)

func TestValidCountsUnicodeCharacters(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("测", 256), strings.Repeat("🧪", 256), string([]byte{0xff})} {
		if Valid(name) {
			t.Fatalf("accepted invalid name of %d bytes", len(name))
		}
	}
	for _, name := range []string{"Peer-测试", strings.Repeat("测", 255), strings.Repeat("🧪", 255), "peer__name", " peer name "} {
		if !Valid(name) {
			t.Fatalf("rejected valid name of %d bytes", len(name))
		}
	}
}

func TestRuntimeNamesPreserveLegacyAndEncodeUnicode(t *testing.T) {
	for _, test := range []struct{ name, key, prefix string }{
		{"Peer-测试", "mcp_68d87f931a770c47d39b414c", "mcp_68d87f931a770c47d39b414c"},
		{"weather.service", "weather.service", "weather_service"},
		{"peer__name", "peer__name", "peer__name"},
	} {
		if ServerKey(test.name) != test.key || ToolPrefix(test.name) != test.prefix {
			t.Fatalf("unexpected mapping for %q: %q / %q", test.name, ServerKey(test.name), ToolPrefix(test.name))
		}
	}
	if ToolPrefix("Peer-测试") == ToolPrefix("peer-测试") || ToolPrefix("é") == ToolPrefix("e\u0301") {
		t.Fatal("distinct original names collapsed")
	}
}
