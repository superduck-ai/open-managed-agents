package environments

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

const (
	packageManifestPath     = "/tmp/open-managed-agents/packages.v1.json"
	packageProvisionerPath  = "/tmp/open-managed-agents/package-provisioner.v1.py"
	packageProvisionCommand = "python3 " + packageProvisionerPath + " " + packageManifestPath
)

//go:embed package_provisioner.v1.py
var packageProvisionerV1 []byte

var packageCredentialURLPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^/@\s]+@`)

type packageManifest struct {
	Version  int                 `json:"version"`
	Packages environmentPackages `json:"packages"`
}

type environmentPackages struct {
	Type  string   `json:"type"`
	APT   []string `json:"apt,omitempty"`
	Cargo []string `json:"cargo,omitempty"`
	Gem   []string `json:"gem,omitempty"`
	Go    []string `json:"go,omitempty"`
	NPM   []string `json:"npm,omitempty"`
	PIP   []string `json:"pip,omitempty"`
}

func buildPackageManifest(config json.RawMessage) ([]byte, bool, error) {
	var cloud struct {
		Type     string          `json:"type"`
		Packages json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(config, &cloud); err != nil {
		return nil, false, fmt.Errorf("decode environment config: %w", err)
	}
	if cloud.Type != "cloud" {
		return nil, false, nil
	}
	packages, err := decodeEnvironmentPackages(cloud.Packages)
	if err != nil {
		return nil, false, fmt.Errorf("decode environment packages: %w", err)
	}
	if packages.Type != "" && packages.Type != managerPackageType {
		return nil, false, errors.New(invalidPackagesTypeMessage)
	}
	if packages.hasCredentialURL() {
		return nil, false, errors.New("config.packages entries must not contain URL credentials")
	}
	if packages.hasManagerOption() {
		return nil, false, errors.New(invalidPackageOptionMessage)
	}
	if packages.empty() {
		return nil, false, nil
	}
	packages.Type = managerPackageType
	data, err := json.Marshal(packageManifest{Version: 1, Packages: packages})
	if err != nil {
		return nil, false, fmt.Errorf("encode packages manifest: %w", err)
	}
	return data, true, nil
}

func decodeEnvironmentPackages(raw json.RawMessage) (environmentPackages, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return environmentPackages{}, nil
	}
	var packages environmentPackages
	if err := json.Unmarshal(raw, &packages); err != nil {
		var legacy []json.RawMessage
		if legacyErr := json.Unmarshal(raw, &legacy); legacyErr == nil && len(legacy) == 0 {
			return environmentPackages{}, nil
		}
		return environmentPackages{}, err
	}
	return packages, nil
}

func (p environmentPackages) hasCredentialURL() bool {
	for _, specs := range p.specsByManager() {
		for _, spec := range specs {
			if packageCredentialURLPattern.MatchString(spec) {
				return true
			}
		}
	}
	return false
}

func (p environmentPackages) hasManagerOption() bool {
	for _, specs := range p.specsByManager() {
		if hasPackageManagerOption(specs) {
			return true
		}
	}
	return false
}

func (p environmentPackages) specsByManager() [][]string {
	return [][]string{p.APT, p.Cargo, p.Gem, p.Go, p.NPM, p.PIP}
}

func (p environmentPackages) empty() bool {
	return len(p.APT) == 0 && len(p.Cargo) == 0 && len(p.Gem) == 0 &&
		len(p.Go) == 0 && len(p.NPM) == 0 && len(p.PIP) == 0
}
