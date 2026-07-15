package environments

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var allowedHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

// normalizeEnvironmentConfigForCreate applies the Environment Config creation
// contract and returns the canonical representation stored for an Environment.
func normalizeEnvironmentConfigForCreate(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return defaultCloudEnvironmentConfig(), nil
	}
	return normalizeFullEnvironmentConfig(raw)
}

// normalizeEnvironmentConfigForUpdate applies an Environment Config patch to
// the current canonical representation.
func normalizeEnvironmentConfigForUpdate(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return defaultCloudEnvironmentConfig(), nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config must be an object")
	}
	configType := rawStringOrEmpty(fields["type"])
	if configType == "" {
		configType = rawConfigType(current)
	}
	if configType == "self_hosted" {
		return marshalRaw(map[string]any{"type": "self_hosted"})
	}
	if configType != "cloud" {
		return nil, errors.New("config.type must be cloud or self_hosted")
	}
	base := normalizedCloudValue(defaultCloudEnvironmentConfig())
	if rawConfigType(current) == "cloud" {
		base = normalizedCloudValue(current)
	}
	if rawPackages, ok := fields["packages"]; ok {
		packages, err := normalizePackages(rawPackages)
		if err != nil {
			return nil, err
		}
		base["packages"] = packages
	}
	if rawNetworking, ok := fields["networking"]; ok {
		networking, err := normalizeNetworking(rawNetworking)
		if err != nil {
			return nil, err
		}
		base["networking"] = networking
	}
	return marshalRaw(base)
}

// environmentConfigForResponse expands a stored Environment Config to the
// compatibility shape exposed by the Environment API.
func environmentConfigForResponse(raw json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return defaultCloudEnvironmentConfigForResponse()
	}
	configType := rawStringOrEmpty(fields["type"])
	switch configType {
	case "self_hosted":
		out, _ := marshalRaw(map[string]any{"type": "self_hosted"})
		return out
	case "", "cloud":
		out, _ := marshalRaw(map[string]any{
			"type":        "cloud",
			"packages":    packagesForResponse(fields["packages"]),
			"networking":  networkingForResponse(fields["networking"]),
			"init_script": rawStringOrEmpty(fields["init_script"]),
			"environment": platformObjectForResponse(fields["environment"]),
		})
		return out
	default:
		return raw
	}
}

// defaultCloudEnvironmentConfig returns the canonical storage representation
// used when a Cloud Environment Config is omitted or reset.
func defaultCloudEnvironmentConfig() json.RawMessage {
	raw, _ := marshalRaw(map[string]any{
		"type":       "cloud",
		"packages":   emptyPackages(),
		"networking": map[string]any{"type": "unrestricted"},
	})
	return raw
}

func normalizeFullEnvironmentConfig(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config must be an object")
	}
	configType := rawStringOrEmpty(fields["type"])
	if configType == "" {
		configType = "cloud"
	}
	switch configType {
	case "self_hosted":
		return marshalRaw(map[string]any{"type": "self_hosted"})
	case "cloud":
		packages, err := normalizePackages(fields["packages"])
		if err != nil {
			return nil, err
		}
		networking, err := normalizeNetworking(fields["networking"])
		if err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"type": "cloud", "packages": packages, "networking": networking})
	default:
		return nil, errors.New("config.type must be cloud or self_hosted")
	}
}

func defaultCloudEnvironmentConfigForResponse() json.RawMessage {
	out, _ := marshalRaw(map[string]any{
		"type":        "cloud",
		"packages":    packagesForResponse(nil),
		"networking":  networkingForResponse(nil),
		"init_script": "",
		"environment": map[string]any{},
	})
	return out
}

func normalizedCloudValue(raw json.RawMessage) map[string]any {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		value = map[string]any{}
	}
	if _, ok := value["packages"]; !ok {
		value["packages"] = emptyPackages()
	}
	if _, ok := value["networking"]; !ok {
		value["networking"] = map[string]any{"type": "unrestricted"}
	}
	value["type"] = "cloud"
	return value
}

func emptyPackages() map[string]any {
	return map[string]any{
		"type":  "packages",
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
	out := emptyPackages()
	for _, manager := range []string{"apt", "cargo", "gem", "go", "npm", "pip"} {
		rawList, ok := fields[manager]
		if !ok || isJSONNull(rawList) {
			continue
		}
		values, err := configStringArray(rawList, "config.packages."+manager)
		if err != nil {
			return nil, err
		}
		out[manager] = values
	}
	return out, nil
}

func normalizeNetworking(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{"type": "unrestricted"}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config.networking must be an object or null")
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = "unrestricted"
	}
	switch networkType {
	case "unrestricted":
		return map[string]any{"type": "unrestricted"}, nil
	case "limited":
		hosts := []string{}
		if rawHosts, ok := fields["allowed_hosts"]; ok && !isJSONNull(rawHosts) {
			values, err := configStringArray(rawHosts, "config.networking.allowed_hosts")
			if err != nil {
				return nil, err
			}
			for _, host := range values {
				if err := validateAllowedHost(host); err != nil {
					return nil, err
				}
			}
			hosts = values
		}
		allowMCP, err := optionalConfigBool(fields["allow_mcp_servers"], false, "config.networking.allow_mcp_servers")
		if err != nil {
			return nil, err
		}
		allowPackages, err := optionalConfigBool(fields["allow_package_managers"], false, "config.networking.allow_package_managers")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":                   "limited",
			"allowed_hosts":          hosts,
			"allow_mcp_servers":      allowMCP,
			"allow_package_managers": allowPackages,
		}, nil
	default:
		return nil, errors.New("config.networking.type must be unrestricted or limited")
	}
}

func validateAllowedHost(host string) error {
	if strings.Contains(host, "://") || strings.Contains(host, "/") || !allowedHostPattern.MatchString(host) {
		return errors.New("config.networking.allowed_hosts entries must be hostnames without URL schemes")
	}
	if len(host) > 253 {
		return errors.New("config.networking.allowed_hosts entries must be at most 253 characters")
	}
	return nil
}

func packagesForResponse(raw json.RawMessage) map[string]any {
	var fields map[string]json.RawMessage
	if len(raw) > 0 && !isJSONNull(raw) {
		_ = json.Unmarshal(raw, &fields)
	}
	out := emptyPackages()
	for _, manager := range []string{"apt", "cargo", "gem", "go", "npm", "pip"} {
		out[manager] = stringArrayForResponse(fields[manager])
	}
	return out
}

func networkingForResponse(raw json.RawMessage) map[string]any {
	var fields map[string]json.RawMessage
	if len(raw) > 0 && !isJSONNull(raw) {
		_ = json.Unmarshal(raw, &fields)
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = "limited"
	}
	return map[string]any{
		"type":                   networkType,
		"allow_mcp_servers":      boolForResponse(fields["allow_mcp_servers"]),
		"allow_package_managers": boolForResponse(fields["allow_package_managers"]),
		"allowed_hosts":          stringArrayForResponse(fields["allowed_hosts"]),
	}
}

func stringArrayForResponse(raw json.RawMessage) []string {
	var values []string
	if len(raw) == 0 || isJSONNull(raw) {
		return []string{}
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	if values == nil {
		return []string{}
	}
	return values
}

func boolForResponse(raw json.RawMessage) bool {
	var value bool
	if len(raw) == 0 || isJSONNull(raw) {
		return false
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return value
}

func configStringArray(raw json.RawMessage, name string) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New(name + " must be an array of strings")
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, errors.New(name + " entries must be non-empty strings")
		}
		if len(value) > 255 {
			return nil, errors.New(name + " entries must be at most 255 characters")
		}
	}
	return values, nil
}

func optionalConfigBool(raw json.RawMessage, fallback bool, name string) (bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return fallback, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, errors.New(name + " must be a boolean")
	}
	return value, nil
}

func rawConfigType(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	return rawStringOrEmpty(fields["type"])
}
