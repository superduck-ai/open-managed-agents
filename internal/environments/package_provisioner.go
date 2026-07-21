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

type cloudEnvironmentPackagesConfig struct {
	Type     string               `json:"type"`
	Packages *environmentPackages `json:"packages"`
}

func buildPackageManifest(config json.RawMessage) ([]byte, bool, error) {
	var cloud cloudEnvironmentPackagesConfig
	if err := json.Unmarshal(config, &cloud); err != nil {
		return nil, false, fmt.Errorf("decode environment config: %w", err)
	}
	if cloud.Type != "cloud" {
		return nil, false, nil
	}
	packages := cloud.Packages
	if packages == nil {
		return nil, false, nil
	}
	packages.ensureLists()
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
	data, err := json.Marshal(packageManifest{Version: 1, Packages: *packages})
	if err != nil {
		return nil, false, fmt.Errorf("encode packages manifest: %w", err)
	}
	return data, true, nil
}

func (p *environmentPackages) empty() bool {
	return len(p.APT) == 0 && len(p.Cargo) == 0 && len(p.Gem) == 0 &&
		len(p.Go) == 0 && len(p.NPM) == 0 && len(p.PIP) == 0
}
