package telemetry

import "regexp"

const OtherValue = "other"

var tokenPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

type Enum map[string]struct{}

func NewEnum(values ...string) Enum {
	enum := make(Enum, len(values))
	for _, value := range values {
		enum[value] = struct{}{}
	}
	return enum
}

func (e Enum) Normalize(value string) string {
	if _, ok := e[value]; ok {
		return value
	}
	return OtherValue
}

func Token(value string) string {
	if tokenPattern.MatchString(value) {
		return value
	}
	return OtherValue
}
