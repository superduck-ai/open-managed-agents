package environmentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	TypeCloud      = "cloud"
	TypeSelfHosted = "self_hosted"

	NetworkTypeLimited      = "limited"
	NetworkTypeUnrestricted = "unrestricted"
)

var allowedHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

// Config is the typed Environment configuration above the raw JSONB storage boundary.
type Config struct {
	Type        string         `json:"type"`
	Packages    *Packages      `json:"packages,omitempty"`
	Networking  *Networking    `json:"networking,omitempty"`
	InitScript  string         `json:"init_script,omitempty"`
	Environment map[string]any `json:"environment,omitempty"`
	extraFields map[string]json.RawMessage
}

type Packages struct {
	Type        string   `json:"type"`
	APT         []string `json:"apt"`
	Cargo       []string `json:"cargo"`
	Gem         []string `json:"gem"`
	Go          []string `json:"go"`
	NPM         []string `json:"npm"`
	PIP         []string `json:"pip"`
	extraFields map[string]json.RawMessage
}

type Networking struct {
	Type                 string   `json:"type"`
	AllowedHosts         []string `json:"allowed_hosts,omitempty"`
	AllowPackageManagers bool     `json:"allow_package_managers,omitempty"`
	AllowMCPServers      bool     `json:"allow_mcp_servers,omitempty"`
	extraFields          map[string]json.RawMessage
}

type Response struct {
	Type        string              `json:"type"`
	Packages    *Packages           `json:"packages,omitempty"`
	Networking  *NetworkingResponse `json:"networking,omitempty"`
	InitScript  *string             `json:"init_script,omitempty"`
	Environment *map[string]any     `json:"environment,omitempty"`
	raw         json.RawMessage
}

type NetworkingResponse struct {
	Type                 string   `json:"type"`
	AllowedHosts         []string `json:"allowed_hosts"`
	AllowPackageManagers bool     `json:"allow_package_managers"`
	AllowMCPServers      bool     `json:"allow_mcp_servers"`
}

func DefaultCloud() Config {
	return Config{
		Type:       TypeCloud,
		Packages:   emptyPackages(),
		Networking: &Networking{Type: NetworkTypeUnrestricted},
	}
}

func DefaultCloudStored() json.RawMessage {
	raw, _ := EncodeStored(DefaultCloud())
	return raw
}

func NormalizeCreate(raw json.RawMessage) (Config, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return DefaultCloud(), nil
	}
	return normalizeFull(raw)
}

func NormalizeUpdate(currentRaw json.RawMessage, raw json.RawMessage) (Config, error) {
	if isJSONNull(raw) {
		return DefaultCloud(), nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Config{}, errors.New("config must be an object")
	}
	current, _ := DecodeStored(currentRaw)
	configType := rawStringOrEmpty(fields["type"])
	if configType == "" {
		configType = current.Type
	}
	if configType == TypeSelfHosted {
		return Config{Type: TypeSelfHosted}, nil
	}
	if configType != TypeCloud {
		return Config{}, errors.New("config.type must be cloud or self_hosted")
	}
	base := DefaultCloud()
	if current.Type == TypeCloud {
		base = normalizedCloud(current)
	}
	if rawPackages, ok := fields["packages"]; ok {
		packages, err := normalizePackages(rawPackages)
		if err != nil {
			return Config{}, err
		}
		base.Packages = packages
	}
	if rawNetworking, ok := fields["networking"]; ok {
		networking, err := normalizeNetworking(rawNetworking)
		if err != nil {
			return Config{}, err
		}
		base.Networking = networking
	}
	return base, nil
}

func ResponseFromStored(raw json.RawMessage) Response {
	config, err := DecodeStored(raw)
	if err != nil {
		return responseFromConfig(Config{Type: TypeCloud})
	}
	if config.Type != "" && config.Type != TypeCloud && config.Type != TypeSelfHosted {
		return Response{raw: append(json.RawMessage(nil), raw...)}
	}
	return responseFromConfig(config)
}

func normalizeFull(raw json.RawMessage) (Config, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Config{}, errors.New("config must be an object")
	}
	configType := rawStringOrEmpty(fields["type"])
	if configType == "" {
		configType = TypeCloud
	}
	switch configType {
	case TypeSelfHosted:
		return Config{Type: TypeSelfHosted}, nil
	case TypeCloud:
		packages, err := normalizePackages(fields["packages"])
		if err != nil {
			return Config{}, err
		}
		networking, err := normalizeNetworking(fields["networking"])
		if err != nil {
			return Config{}, err
		}
		return Config{Type: TypeCloud, Packages: packages, Networking: networking}, nil
	default:
		return Config{}, errors.New("config.type must be cloud or self_hosted")
	}
}

func normalizedCloud(config Config) Config {
	config.Type = TypeCloud
	if config.Packages == nil {
		config.Packages = emptyPackages()
	}
	if config.Networking == nil {
		config.Networking = &Networking{Type: NetworkTypeUnrestricted}
	}
	return config
}

func emptyPackages() *Packages {
	return &Packages{
		Type:  "packages",
		APT:   []string{},
		Cargo: []string{},
		Gem:   []string{},
		Go:    []string{},
		NPM:   []string{},
		PIP:   []string{},
	}
}

func normalizePackages(raw json.RawMessage) (*Packages, error) {
	packages := emptyPackages()
	if len(raw) == 0 || isJSONNull(raw) {
		return packages, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config.packages must be an object or null")
	}
	managers := []struct {
		name   string
		target *[]string
	}{
		{name: "apt", target: &packages.APT},
		{name: "cargo", target: &packages.Cargo},
		{name: "gem", target: &packages.Gem},
		{name: "go", target: &packages.Go},
		{name: "npm", target: &packages.NPM},
		{name: "pip", target: &packages.PIP},
	}
	for _, manager := range managers {
		rawList, ok := fields[manager.name]
		if !ok || isJSONNull(rawList) {
			continue
		}
		values, err := stringArray(rawList, "config.packages."+manager.name)
		if err != nil {
			return nil, err
		}
		*manager.target = values
	}
	return packages, nil
}

func normalizeNetworking(raw json.RawMessage) (*Networking, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return &Networking{Type: NetworkTypeUnrestricted}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config.networking must be an object or null")
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = NetworkTypeUnrestricted
	}
	switch networkType {
	case NetworkTypeUnrestricted:
		return &Networking{Type: NetworkTypeUnrestricted}, nil
	case NetworkTypeLimited:
		hosts := []string{}
		if rawHosts, ok := fields["allowed_hosts"]; ok && !isJSONNull(rawHosts) {
			values, err := stringArray(rawHosts, "config.networking.allowed_hosts")
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
		allowMCP, err := optionalBool(fields["allow_mcp_servers"], false, "config.networking.allow_mcp_servers")
		if err != nil {
			return nil, err
		}
		allowPackages, err := optionalBool(fields["allow_package_managers"], false, "config.networking.allow_package_managers")
		if err != nil {
			return nil, err
		}
		return &Networking{
			Type:                 NetworkTypeLimited,
			AllowedHosts:         hosts,
			AllowMCPServers:      allowMCP,
			AllowPackageManagers: allowPackages,
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

func stringArray(raw json.RawMessage, name string) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s entries must be non-empty strings", name)
		}
		if len(value) > 255 {
			return nil, fmt.Errorf("%s entries must be at most 255 characters", name)
		}
	}
	return values, nil
}

func optionalBool(raw json.RawMessage, fallback bool, name string) (bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return fallback, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func rawStringOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

func responseFromConfig(config Config) Response {
	if config.Type == TypeSelfHosted {
		return Response{Type: TypeSelfHosted}
	}
	packages := responsePackages(config.Packages)
	networking := responseNetworking(config.Networking)
	initScript := config.InitScript
	environment := config.Environment
	if environment == nil {
		environment = map[string]any{}
	}
	return Response{
		Type:        TypeCloud,
		Packages:    packages,
		Networking:  networking,
		InitScript:  &initScript,
		Environment: &environment,
	}
}

func responsePackages(packages *Packages) *Packages {
	if packages == nil {
		packages = emptyPackages()
	}
	return &Packages{
		Type:  "packages",
		APT:   nonNilStrings(packages.APT),
		Cargo: nonNilStrings(packages.Cargo),
		Gem:   nonNilStrings(packages.Gem),
		Go:    nonNilStrings(packages.Go),
		NPM:   nonNilStrings(packages.NPM),
		PIP:   nonNilStrings(packages.PIP),
	}
}

func responseNetworking(networking *Networking) *NetworkingResponse {
	if networking == nil {
		return &NetworkingResponse{Type: NetworkTypeLimited, AllowedHosts: []string{}}
	}
	networkType := networking.Type
	if networkType == "" {
		networkType = NetworkTypeLimited
	}
	return &NetworkingResponse{
		Type:                 networkType,
		AllowedHosts:         nonNilStrings(networking.AllowedHosts),
		AllowPackageManagers: networking.AllowPackageManagers,
		AllowMCPServers:      networking.AllowMCPServers,
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
