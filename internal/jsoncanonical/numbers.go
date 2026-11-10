package jsoncanonical

import (
	"encoding/json"
	"math/big"
	"strings"
)

func normalizeNumbers(value any) any {
	switch value := value.(type) {
	case json.Number:
		return normalizeNumber(string(value))
	case []any:
		for i := range value {
			value[i] = normalizeNumbers(value[i])
		}
	case map[string]any:
		for key := range value {
			value[key] = normalizeNumbers(value[key])
		}
	}
	return value
}

func normalizeNumber(value string) json.Number {
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign, value = "-", value[1:]
	}
	var exponent big.Int
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent.SetString(value[index+1:], 10)
		value = value[:index]
	}
	if index := strings.IndexByte(value, '.'); index >= 0 {
		exponent.Sub(&exponent, big.NewInt(int64(len(value)-index-1)))
		value = value[:index] + value[index+1:]
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return json.Number("0")
	}
	coefficient := strings.TrimRight(value, "0")
	exponent.Add(&exponent, big.NewInt(int64(len(value)-len(coefficient))))
	if exponent.Sign() == 0 {
		return json.Number(sign + coefficient)
	}
	return json.Number(sign + coefficient + "e" + exponent.String())
}
