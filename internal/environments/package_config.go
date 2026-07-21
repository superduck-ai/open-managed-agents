package environments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
)

var packageManagerNames = [...]string{"apt", "cargo", "gem", "go", "npm", "pip"}

const (
	managerPackageType          = "packages"
	invalidPackagesTypeMessage  = `config.packages.type must be "packages"`
	invalidPackageOptionMessage = "config.packages entries must be package specs, not manager options"
)

type environmentPackages struct {
	Type  string   `json:"type"`
	APT   []string `json:"apt"`
	Cargo []string `json:"cargo"`
	Gem   []string `json:"gem"`
	Go    []string `json:"go"`
	NPM   []string `json:"npm"`
	PIP   []string `json:"pip"`
}

func emptyPackages() *environmentPackages {
	return &environmentPackages{
		Type:  managerPackageType,
		APT:   []string{},
		Cargo: []string{},
		Gem:   []string{},
		Go:    []string{},
		NPM:   []string{},
		PIP:   []string{},
	}
}

func normalizePackages(raw json.RawMessage) (*environmentPackages, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return emptyPackages(), nil
	}
	packages := emptyPackages()
	if err := json.Unmarshal(raw, packages); err != nil {
		return nil, errors.New("config.packages must be an object or null")
	}
	if packages.Type != managerPackageType {
		return nil, errors.New(invalidPackagesTypeMessage)
	}
	for manager, values := range packages.specsByManagerName() {
		if hasPackageCredentialURL(values) {
			return nil, fmt.Errorf("config.packages.%s entries must not contain URL credentials", manager)
		}
		if hasPackageManagerOption(values) {
			return nil, errors.New(invalidPackageOptionMessage)
		}
	}
	packages.ensureLists()
	return packages, nil
}

func hasPackageManagerOption(specs []string) bool {
	return lo.SomeBy(specs, func(spec string) bool {
		return strings.HasPrefix(strings.TrimSpace(spec), "-")
	})
}

func hasPackageCredentialURL(specs []string) bool {
	return lo.SomeBy(specs, packageCredentialURLPattern.MatchString)
}

func (p *environmentPackages) specsByManagerName() map[string][]string {
	return map[string][]string{
		"apt": p.APT, "cargo": p.Cargo, "gem": p.Gem,
		"go": p.Go, "npm": p.NPM, "pip": p.PIP,
	}
}

func (p *environmentPackages) hasCredentialURL() bool {
	return lo.SomeBy(p.specsByManager(), hasPackageCredentialURL)
}

func (p *environmentPackages) hasManagerOption() bool {
	return lo.SomeBy(p.specsByManager(), hasPackageManagerOption)
}

func (p *environmentPackages) specsByManager() [][]string {
	return [][]string{p.APT, p.Cargo, p.Gem, p.Go, p.NPM, p.PIP}
}

func (p *environmentPackages) ensureLists() {
	if p.APT == nil {
		p.APT = []string{}
	}
	if p.Cargo == nil {
		p.Cargo = []string{}
	}
	if p.Gem == nil {
		p.Gem = []string{}
	}
	if p.Go == nil {
		p.Go = []string{}
	}
	if p.NPM == nil {
		p.NPM = []string{}
	}
	if p.PIP == nil {
		p.PIP = []string{}
	}
}
