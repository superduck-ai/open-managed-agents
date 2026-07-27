package environments

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/samber/lo"
)

const (
	managerPackageType          = "packages"
	maxPackageSpecLength        = 255
	invalidPackagesTypeMessage  = `config.packages.type must be "packages"`
	invalidPackageOptionMessage = "config.packages entries must be package specs, not manager options"
)

var packageCredentialURLPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^/@\s]+@`)

type environmentPackages struct {
	Type  string   `json:"type"`
	APT   []string `json:"apt"`
	Cargo []string `json:"cargo"`
	Gem   []string `json:"gem"`
	Go    []string `json:"go"`
	NPM   []string `json:"npm"`
	PIP   []string `json:"pip"`
}

// managerSpecs 把一个 Package Manager 的名字和它的 spec 列表绑在一起。specs 是
// 指针，使 ensureLists 能就地补齐 nil 列表。
type managerSpecs struct {
	manager string
	specs   *[]string
}

// specsByManager 是六类 Package Manager 的唯一真相源。校验、空判断和 provisioner
// 的安装顺序都从这里派生，因此顺序固定为 apt → cargo → gem → go → npm → pip。
func (p *environmentPackages) specsByManager() []managerSpecs {
	return []managerSpecs{
		{manager: "apt", specs: &p.APT},
		{manager: "cargo", specs: &p.Cargo},
		{manager: "gem", specs: &p.Gem},
		{manager: "go", specs: &p.Go},
		{manager: "npm", specs: &p.NPM},
		{manager: "pip", specs: &p.PIP},
	}
}

func emptyPackages() *environmentPackages {
	return (&environmentPackages{}).normalized()
}

// normalized 固定 Claude 兼容响应与 provisioner manifest 的字段形状：把 type 补成
// managerPackageType，并让每个 manager 序列化成 [] 而非 null。返回自身便于链式调用。
func (p *environmentPackages) normalized() *environmentPackages {
	p.Type = managerPackageType
	p.ensureLists()
	return p
}

// normalizePackages 是 config.packages 的 HTTP 边界：它接受任意请求 JSON，
// 判断结构、校验语义，并返回已补齐空列表的命名 schema。存量配置改走
// buildPackageManifest 的类型化解码，不再经过这里。
func normalizePackages(raw json.RawMessage) (*environmentPackages, error) {
	packages := emptyPackages()
	if len(raw) == 0 || isJSONNull(raw) {
		return packages, nil
	}
	if err := json.Unmarshal(raw, packages); err != nil {
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) && typeError.Field != "" {
			return nil, fmt.Errorf("config.packages.%s must be an array of strings", typeError.Field)
		}
		return nil, errors.New("config.packages must be an object or null")
	}
	if err := packages.validate(); err != nil {
		return nil, err
	}
	packages.ensureLists()
	return packages, nil
}

func (p *environmentPackages) validate() error {
	if p.Type != "" && p.Type != managerPackageType {
		return errors.New(invalidPackagesTypeMessage)
	}
	for _, entry := range p.specsByManager() {
		if err := validatePackageSpecs(entry.manager, *entry.specs); err != nil {
			return err
		}
	}
	return nil
}

// validatePackageSpecs 只报告违规的 manager 和规则，不回显 spec 本身，
// 避免把私有仓库地址或 token 写进 API 错误和日志。
func validatePackageSpecs(manager string, specs []string) error {
	switch {
	case lo.SomeBy(specs, isBlankPackageSpec):
		return fmt.Errorf("config.packages.%s entries must be non-empty strings", manager)
	case lo.SomeBy(specs, isOverlongPackageSpec):
		return fmt.Errorf("config.packages.%s entries must be at most %d characters", manager, maxPackageSpecLength)
	case lo.SomeBy(specs, isPackageManagerOption):
		return errors.New(invalidPackageOptionMessage)
	case lo.SomeBy(specs, packageCredentialURLPattern.MatchString):
		return fmt.Errorf("config.packages.%s entries must not contain URL credentials", manager)
	}
	return nil
}

func isBlankPackageSpec(spec string) bool {
	return strings.TrimSpace(spec) == ""
}

func isOverlongPackageSpec(spec string) bool {
	return len(spec) > maxPackageSpecLength
}

func isPackageManagerOption(spec string) bool {
	return strings.HasPrefix(strings.TrimSpace(spec), "-")
}

func (p *environmentPackages) empty() bool {
	return lo.EveryBy(p.specsByManager(), func(entry managerSpecs) bool {
		return len(*entry.specs) == 0
	})
}

// ensureLists 让每个 manager 都序列化成 []，而不是 null，使 Claude 兼容响应和
// provisioner manifest 的字段形状保持稳定。
func (p *environmentPackages) ensureLists() {
	for _, entry := range p.specsByManager() {
		if *entry.specs == nil {
			*entry.specs = []string{}
		}
	}
}
