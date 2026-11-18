package titletext

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var terminalControl = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_]`)

func Normalize(text string, maxBytes int) string {
	text = terminalControl.ReplaceAllString(text, "")
	text = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && !unicode.IsSpace(r)) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	return Clip(strings.Join(strings.Fields(text), " "), maxBytes)
}

func Clip(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	for !utf8.RuneStart(text[maxBytes]) {
		maxBytes--
	}
	return strings.TrimRightFunc(text[:maxBytes], unicode.IsSpace)
}

func Generated(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, marker := range []string{"***", "**", "*"} {
			if len(line) > 2*len(marker) && strings.HasPrefix(line, marker) && strings.HasSuffix(line, marker) {
				inner := line[len(marker) : len(line)-len(marker)]
				if !strings.Contains(inner, "*") {
					line = inner
					break
				}
			}
		}
		if title := Normalize(line, 120); title != "" {
			return title
		}
	}
	return ""
}

func Fallback(text string) string {
	words := strings.Fields(text)
	if len(words) > 8 {
		words = words[:8]
	}
	return Normalize(strings.Join(words, " "), 96)
}
