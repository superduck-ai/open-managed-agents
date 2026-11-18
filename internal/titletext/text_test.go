package titletext

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTitleText(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\n \n", ""},
		{"\x1b[31m\u202e\x00\u200b\x1b[0m", ""},
		{"\n **修复会话标题**\nextra explanation", "修复会话标题"},
		{"***Fix session naming***", "Fix session naming"},
		{"hello\t world", "hello world"},
		{"\x1b]0;hidden\x07Visible\x1b[0m", "Visible"},
	} {
		if got := Generated(tc.input); got != tc.want {
			t.Errorf("Generated(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
	title := Generated(strings.Repeat("中", 100))
	if len(title) != 120 || !utf8.ValidString(title) {
		t.Fatalf("invalid title %q", title)
	}
	if got := Fallback("one two three four five six seven eight nine"); got != "one two three four five six seven eight" {
		t.Fatal(got)
	}
	if got := Fallback(strings.Repeat("中", 100)); len(got) != 96 || !utf8.ValidString(got) {
		t.Fatal(got)
	}
}
