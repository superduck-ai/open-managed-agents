package db

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type rawDBUUIDArgument struct {
	value    *string
	required bool
}

func dbUUID(value string) rawDBUUIDArgument {
	return rawDBUUIDArgument{value: &value, required: true}
}

func dbNullableUUID(value *string) rawDBUUIDArgument {
	return rawDBUUIDArgument{value: value}
}

func (argument rawDBUUIDArgument) typedValue(name string) (any, error) {
	if argument.value == nil || strings.TrimSpace(*argument.value) == "" {
		if argument.required {
			return nil, fmt.Errorf("%s must be a non-nil UUID", name)
		}
		return uuid.NullUUID{}, nil
	}
	parsed, err := parseDBUUID(name, *argument.value)
	if err != nil {
		return nil, err
	}
	if argument.required {
		return parsed, nil
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func tryParseDBUUIDIdentifier(value string) uuid.NullUUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}
}

func parseDBUUID(name, value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s must be a non-nil UUID", name)
	}
	return parsed, nil
}

func parseDBNullableUUID(name string, value *string) (uuid.NullUUID, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return uuid.NullUUID{}, nil
	}
	parsed, err := parseDBUUID(name, *value)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func typedDBUUIDArguments(arguments map[string]any) (map[string]any, error) {
	var typed map[string]any
	for name, value := range arguments {
		argument, ok := value.(rawDBUUIDArgument)
		if !ok {
			continue
		}
		typedValue, err := argument.typedValue(name)
		if err != nil {
			return nil, err
		}
		if typed == nil {
			typed = make(map[string]any, len(arguments))
			for existingName, existingValue := range arguments {
				typed[existingName] = existingValue
			}
		}
		typed[name] = typedValue
	}
	if typed == nil {
		return arguments, nil
	}
	return typed, nil
}

func nullableUUIDString(value uuid.NullUUID) *string {
	if !value.Valid {
		return nil
	}
	text := value.UUID.String()
	return &text
}

func nullableUUIDValue(value uuid.NullUUID) string {
	if !value.Valid {
		return ""
	}
	return value.UUID.String()
}
