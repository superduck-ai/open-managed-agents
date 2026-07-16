package environmentconfig

import "encoding/json"

func (config *Config) UnmarshalJSON(data []byte) error {
	type plainConfig Config
	if err := json.Unmarshal(data, (*plainConfig)(config)); err != nil {
		return err
	}
	config.extraFields = unknownFields(data, "type", "packages", "networking", "init_script", "environment")
	return nil
}

func (config Config) MarshalJSON() ([]byte, error) {
	type knownConfig struct {
		Type        string         `json:"type"`
		Packages    *Packages      `json:"packages,omitempty"`
		Networking  *Networking    `json:"networking,omitempty"`
		InitScript  string         `json:"init_script,omitempty"`
		Environment map[string]any `json:"environment,omitempty"`
	}
	return marshalWithExtra(config.extraFields, knownConfig{
		Type: config.Type, Packages: config.Packages, Networking: config.Networking,
		InitScript: config.InitScript, Environment: config.Environment,
	})
}

func (packages *Packages) UnmarshalJSON(data []byte) error {
	type plainPackages Packages
	if err := json.Unmarshal(data, (*plainPackages)(packages)); err != nil {
		return err
	}
	packages.extraFields = unknownFields(data, "type", "apt", "cargo", "gem", "go", "npm", "pip")
	return nil
}

func (packages Packages) MarshalJSON() ([]byte, error) {
	type knownPackages struct {
		Type  string   `json:"type,omitempty"`
		APT   []string `json:"apt"`
		Cargo []string `json:"cargo"`
		Gem   []string `json:"gem"`
		Go    []string `json:"go"`
		NPM   []string `json:"npm"`
		PIP   []string `json:"pip"`
	}
	return marshalWithExtra(packages.extraFields, knownPackages{
		Type: packages.Type, APT: packages.APT, Cargo: packages.Cargo, Gem: packages.Gem,
		Go: packages.Go, NPM: packages.NPM, PIP: packages.PIP,
	})
}

func (networking *Networking) UnmarshalJSON(data []byte) error {
	type plainNetworking Networking
	if err := json.Unmarshal(data, (*plainNetworking)(networking)); err != nil {
		return err
	}
	networking.extraFields = unknownFields(data, "type", "allowed_hosts", "allow_package_managers", "allow_mcp_servers")
	return nil
}

func (networking Networking) MarshalJSON() ([]byte, error) {
	type knownNetworking struct {
		Type                 string   `json:"type"`
		AllowedHosts         []string `json:"allowed_hosts,omitempty"`
		AllowPackageManagers bool     `json:"allow_package_managers,omitempty"`
		AllowMCPServers      bool     `json:"allow_mcp_servers,omitempty"`
	}
	return marshalWithExtra(networking.extraFields, knownNetworking{
		Type: networking.Type, AllowedHosts: networking.AllowedHosts,
		AllowPackageManagers: networking.AllowPackageManagers, AllowMCPServers: networking.AllowMCPServers,
	})
}

func (response Response) MarshalJSON() ([]byte, error) {
	if len(response.raw) > 0 {
		return append([]byte(nil), response.raw...), nil
	}
	type plainResponse Response
	return json.Marshal(plainResponse(response))
}

// DecodeStored decodes the JSONB representation while allowing future fields.
func DecodeStored(raw json.RawMessage) (Config, error) {
	if len(raw) == 0 {
		return Config{}, nil
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func EncodeStored(config Config) (json.RawMessage, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func unknownFields(data []byte, known ...string) map[string]json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil
	}
	for _, name := range known {
		delete(fields, name)
	}
	return fields
}

func cloneFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(fields)+5)
	for name, raw := range fields {
		cloned[name] = append(json.RawMessage(nil), raw...)
	}
	return cloned
}

func marshalWithExtra(extraFields map[string]json.RawMessage, known any) ([]byte, error) {
	knownJSON, err := json.Marshal(known)
	if err != nil {
		return nil, err
	}
	var knownFields map[string]json.RawMessage
	if err := json.Unmarshal(knownJSON, &knownFields); err != nil {
		return nil, err
	}
	fields := cloneFields(extraFields)
	for name, raw := range knownFields {
		fields[name] = raw
	}
	return json.Marshal(fields)
}
