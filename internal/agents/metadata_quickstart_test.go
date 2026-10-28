package agents

import (
	"strings"
	"testing"
)

func TestQuickstartPackageMetadataLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
		valid bool
	}{
		{name: "ordinary over limit", key: "ordinary", value: strings.Repeat("a", 513)},
		{name: "ordinary multibyte over limit", key: "ordinary", value: strings.Repeat("é", 257)},
		{name: "reserved over limit", key: "quickstart_environment_packages", value: strings.Repeat("x", (1<<20)+1)},
		{name: "reserved multibyte over limit", key: "quickstart_environment_packages", value: strings.Repeat("é", (1<<19)+1)},
		{name: "ordinary at limit", key: "ordinary", value: strings.Repeat("a", 512), valid: true},
		{name: "reserved over old limit", key: "quickstart_environment_packages", value: strings.Repeat("a", 513), valid: true},
		{name: "reserved multibyte at limit", key: "quickstart_environment_packages", value: strings.Repeat("é", 1<<19), valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMetadata(map[string]string{tc.key: tc.value})
			if (err == nil) != tc.valid {
				t.Fatalf("validateMetadata: got %v, want valid=%v", err, tc.valid)
			}
		})
	}
}
