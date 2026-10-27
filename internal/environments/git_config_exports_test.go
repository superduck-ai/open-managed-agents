package environments

import (
	"strings"
	"testing"
)

func TestConfiguredGitSSHtoHTTPSEntriesSkipsGitHub(t *testing.T) {
	t.Parallel()
	entries := configuredGitSSHtoHTTPSEntries([]string{"gitlab.xxxx.cn", "github.com", "gitlab.xxxx.cn"})
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2 (one host, two insteadOf)", len(entries))
	}
	lines := gitConfigExportLinesFrom(environmentManagerBuiltInGitConfigCount, entries)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"export GIT_CONFIG_KEY_3='url.https://gitlab.xxxx.cn/.insteadOf'",
		"export GIT_CONFIG_VALUE_3='git@gitlab.xxxx.cn:'",
		"export GIT_CONFIG_KEY_4='url.https://gitlab.xxxx.cn/.insteadOf'",
		"export GIT_CONFIG_VALUE_4='ssh://git@gitlab.xxxx.cn/'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestConfiguredGitSSHtoHTTPSEntriesEmpty(t *testing.T) {
	t.Parallel()
	if got := configuredGitSSHtoHTTPSEntries(nil); len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
	if got := gitConfigExportLinesFrom(3, nil); got != nil {
		t.Fatalf("export lines = %#v", got)
	}
}
