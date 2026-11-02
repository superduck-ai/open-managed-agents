package devinit

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestNextProviderSlug(t *testing.T) {
	for _, test := range []struct {
		base  string
		taken []string
		want  string
	}{
		{"", []string{"default", "default-2"}, "default-3"},
		{"compatible", []string{"compatible", "compatible-2"}, "compatible-3"},
		{"compatible", nil, "compatible"},
		{strings.Repeat("a", 32), []string{strings.Repeat("a", 32), strings.Repeat("a", 30) + "-2"}, strings.Repeat("a", 30) + "-3"},
	} {
		got := nextProviderSlug(test.base, test.taken)
		if got != test.want {
			t.Errorf("nextProviderSlug(%q, %v) = %q, want %q", test.base, test.taken, got, test.want)
		}
		if err := llmproviders.ValidateSlug(got); err != nil {
			t.Fatal(err)
		}
	}
}
