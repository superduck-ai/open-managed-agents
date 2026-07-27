// Package collections provides small, domain-neutral collection operations.
package collections

import (
	"strings"

	"github.com/samber/lo"
)

// UniqueTrimmedStrings trims values, removes blanks, and keeps the first
// occurrence of each value in stable order.
func UniqueTrimmedStrings(values []string) []string {
	trimmed := lo.FilterMap(values, func(value string, _ int) (string, bool) {
		value = strings.TrimSpace(value)
		return value, value != ""
	})
	return lo.Uniq(trimmed)
}

// StringSet returns a membership set for the supplied values.
func StringSet(values []string) map[string]struct{} {
	return lo.SliceToMap(values, func(value string) (string, struct{}) {
		return value, struct{}{}
	})
}
