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
		Type     string              `json:"type"`
		Packages environmentPackages `json:"packages"`
	}
	if err := json.Unmarshal(config, &cloud); err != nil {
		return nil, false, fmt.Errorf("decode environment packages: %w", err)
	}
	if cloud.Type != "cloud" {
		return nil, false, nil
	}
	packages := cloud.Packages
	if packages.Type != "" && packages.Type != "packages" {
		return nil, false, errors.New("config.packages.type must be packages")
	}
	if packages.hasCredentialURL() {
		return nil, false, errors.New("config.packages entries must not contain URL credentials")
	}
	if packages.empty() {
		return nil, false, nil
	}
	packages.Type = "packages"
	data, err := json.Marshal(packageManifest{Version: 1, Packages: packages})
	if err != nil {
		return nil, false, fmt.Errorf("encode packages manifest: %w", err)
	}
	return data, true, nil
}

func (p environmentPackages) hasCredentialURL() bool {
	for _, specs := range [][]string{p.APT, p.Cargo, p.Gem, p.Go, p.NPM, p.PIP} {
		for _, spec := range specs {
			if packageCredentialURLPattern.MatchString(spec) {
				return true
			}
		}
	}
	return false
}

func (p environmentPackages) empty() bool {
	return len(p.APT) == 0 && len(p.Cargo) == 0 && len(p.Gem) == 0 &&
		len(p.Go) == 0 && len(p.NPM) == 0 && len(p.PIP) == 0
}
