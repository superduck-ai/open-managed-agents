package environments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var packageManagerNames = [...]string{"apt", "cargo", "gem", "go", "npm", "pip"}

const (
	managerPackageType          = "packages"
	invalidPackagesTypeMessage  = `config.packages.type must be "packages"`
	invalidPackageOptionMessage = "config.packages entries must be package specs, not manager options"
)

func emptyPackages() map[string]any {
	return map[string]any{
		"type":  managerPackageType,
		"apt":   []string{},
		"cargo": []string{},
		"gem":   []string{},
		"go":    []string{},
		"npm":   []string{},
		"pip":   []string{},
	}
}

func normalizePackages(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return emptyPackages(), nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config.packages must be an object or null")
	}
	if rawType, ok := fields["type"]; ok && !isJSONNull(rawType) {
		var packageType string
		if err := json.Unmarshal(rawType, &packageType); err != nil || packageType != managerPackageType {
			return nil, errors.New(invalidPackagesTypeMessage)
		}
	}
	out := emptyPackages()
	for _, manager := range packageManagerNames {
		rawList, ok := fields[manager]
		if !ok || isJSONNull(rawList) {
			continue
		}
		values, err := stringArray(rawList, "config.packages."+manager)
		if err != nil {
			return nil, err
		}
		if hasPackageCredentialURL(values) {
			return nil, fmt.Errorf("config.packages.%s entries must not contain URL credentials", manager)
		}
		if hasPackageManagerOption(values) {
			return nil, errors.New(invalidPackageOptionMessage)
		}
		out[manager] = values
	}
	return out, nil
}

func hasPackageManagerOption(specs []string) bool {
	for _, spec := range specs {
		if strings.HasPrefix(strings.TrimSpace(spec), "-") {
			return true
		}
	}
	return false
}

func hasPackageCredentialURL(specs []string) bool {
	for _, spec := range specs {
		if packageCredentialURLPattern.MatchString(spec) {
			return true
		}
	}
	return false
}
