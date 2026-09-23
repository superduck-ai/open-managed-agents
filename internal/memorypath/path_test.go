package memorypath

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, value := range []string{"", "/", "relative", "/a//b", "/a/../b", "/a/./b", "/a/", "/cafe\u0301.md", "/a\nb", "/a\tb", "/a\u200bb", "/a\u202eb", "/a\x00b", "/\xff", "/" + strings.Repeat("a", 1024)} {
		t.Run(value, func(t *testing.T) {
			if err := Validate(value); err == nil {
				t.Fatalf("accepted invalid path %q", value)
			}
		})
	}
	for _, value := range []string{"/café.md", "/中文/笔记.md", "/a b/%_.md", "/" + strings.Repeat("a", 1023)} {
		if err := Validate(value); err != nil {
			t.Fatalf("rejected valid path %q: %v", value, err)
		}
	}
}

func TestValidatePrefix(t *testing.T) {
	for _, value := range []string{"/cafe\u0301", "/a\nb", "/a\u200bb", "/a/../", "/a//b", "relative", "/\xff", "/" + strings.Repeat("a", 1024)} {
		if err := ValidatePrefix(value); err == nil {
			t.Fatalf("accepted invalid prefix %q", value)
		}
	}
	for _, value := range []string{"", "/", "/café", "/中文/", "/a b/%_"} {
		if err := ValidatePrefix(value); err != nil {
			t.Fatalf("rejected valid prefix %q: %v", value, err)
		}
	}
}
