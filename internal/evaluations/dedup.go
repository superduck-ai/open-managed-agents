package evaluations

import (
	"strings"
	"unicode"
)

const duplicateSimilarity = 0.85

type queryFingerprint struct {
	text  string
	grams map[string]struct{}
}

func fingerprint(query string) queryFingerprint {
	var runes []rune
	for _, char := range strings.ToLower(query) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			runes = append(runes, char)
		}
	}
	grams := make(map[string]struct{}, len(runes))
	for index := 0; index+1 < len(runes); index++ {
		grams[string(runes[index:index+2])] = struct{}{}
	}
	return queryFingerprint{text: string(runes), grams: grams}
}

func (f queryFingerprint) similar(other queryFingerprint) bool {
	if f.text == other.text {
		return true
	}
	if len(f.grams) == 0 || len(other.grams) == 0 {
		return false
	}
	shared := 0
	for gram := range f.grams {
		if _, ok := other.grams[gram]; ok {
			shared++
		}
	}
	return float64(2*shared)/float64(len(f.grams)+len(other.grams)) >= duplicateSimilarity
}

type duplicateFilter struct {
	seen []queryFingerprint
}

func newDuplicateFilter(existing []string) *duplicateFilter {
	filter := &duplicateFilter{seen: make([]queryFingerprint, 0, len(existing))}
	for _, query := range existing {
		filter.seen = append(filter.seen, fingerprint(query))
	}
	return filter
}

func (f *duplicateFilter) admit(query string) bool {
	candidate := fingerprint(query)
	for _, seen := range f.seen {
		if candidate.similar(seen) {
			return false
		}
	}
	f.seen = append(f.seen, candidate)
	return true
}
