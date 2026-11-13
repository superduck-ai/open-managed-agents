package porttunnel

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeCloseReplacesUnsendableCodes(t *testing.T) {
	for _, code := range []int{0, 999, 1004, 1006, 1015, 1016, 2999, 5000} {
		if got, _ := NormalizeClose(code, "x"); got != 1001 {
			t.Fatalf("NormalizeClose(%d) = %d, want 1001", code, got)
		}
	}
	if got, reason := NormalizeClose(1005, "ignored"); got != 1005 || reason != "" {
		t.Fatalf("no-status = %d %q", got, reason)
	}
	long := strings.Repeat("é", 100)
	if _, reason := NormalizeClose(1000, long); len(reason) > 123 || !utf8.ValidString(reason) {
		t.Fatalf("reason length %d valid=%t", len(reason), utf8.ValidString(reason))
	}
}

func TestNormalizeCloseKeepsSendableCodes(t *testing.T) {
	for _, code := range []int{1000, 1001, 1003, 1007, 1011, 1014, 3000, 4001, 4999} {
		if got, reason := NormalizeClose(code, "bye"); got != code || reason != "bye" {
			t.Fatalf("NormalizeClose(%d) = %d %q", code, got, reason)
		}
	}
}
