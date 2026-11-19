package clitools

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

// Package 由部署方登记，清单和可执行文件由工具包提供。
type Package struct {
	Manifest         string `yaml:"manifest"`
	Provider         string `yaml:"oauth_provider"`
	ProviderVariable string `yaml:"provider_env"`
}

// Definition 是 Server 下发给 Brain 的 CLI 调用约束，不包含客户端密钥。
type Definition struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Executable   string   `json:"executable"`
	Variable     string   `json:"oauth_variable"`
	TargetHost   string   `json:"target_host"`
	Subcommands  []string `json:"subcommands"`
	BlockedFlags []string `json:"blocked_flags,omitempty"`
	PublicHelp   bool     `json:"public_help,omitempty"`
}

type Binding struct {
	Definition
	Package
}

type OAuthProviders map[string]string

type OAuthConfig struct {
	Providers OAuthProviders `json:"cli_oauth_providers"`
}

func (c OAuthConfig) ProviderFor(tools []Binding, variable, host string) string {
	for _, tool := range tools {
		if tool.Variable == variable && tool.TargetHost == host {
			return c.Providers[variable]
		}
	}
	return ""
}

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
var variablePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
var executablePattern = regexp.MustCompile(`^([A-Za-z0-9._+-]+|/[A-Za-z0-9._/+ -]+)$`)

func (d Definition) validate() error {
	if !namePattern.MatchString(d.Name) || strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("无效的 CLI 工具名或描述 %q", d.Name)
	}
	if !executablePattern.MatchString(d.Executable) || strings.HasPrefix(d.Executable, "-") {
		return fmt.Errorf("无效的沙箱可执行文件 %q", d.Executable)
	}
	if !variablePattern.MatchString(d.Variable) {
		return fmt.Errorf("无效的 OAuth 变量 %q", d.Variable)
	}
	host, err := networkpolicy.NormalizeHost(d.TargetHost)
	if err != nil || host != d.TargetHost {
		return fmt.Errorf("无效的 OAuth 目标域名 %q", d.TargetHost)
	}
	if len(d.Subcommands) == 0 || slices.Contains(d.Subcommands, "") {
		return fmt.Errorf("CLI %s 需要允许执行的子命令", d.Name)
	}
	return nil
}

func Load(packages []Package, base string) ([]Binding, error) {
	var result []Binding
	names := map[string]bool{"bash": true, "read": true, "write": true, "edit": true, "grep": true, "glob": true, "requestoauth": true, "askuserquestion": true, "subagent": true, "task": true}
	variables := map[string]bool{}
	for _, p := range packages {
		file := p.Manifest
		if !filepath.IsAbs(file) {
			file = filepath.Join(base, file)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var d Definition
		if err = json.Unmarshal(data, &d, json.RejectUnknownMembers(true)); err != nil {
			return nil, fmt.Errorf("读取 CLI 清单 %s: %w", file, err)
		}
		if err := d.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if names[strings.ToLower(d.Name)] || variables[d.Variable] {
			return nil, fmt.Errorf("CLI 清单 %s 的名称或变量已被占用", file)
		}
		if p.Provider == "" && !variablePattern.MatchString(p.ProviderVariable) {
			return nil, fmt.Errorf("CLI %s 缺少 OAuth 客户端引用", d.Name)
		}
		names[strings.ToLower(d.Name)], variables[d.Variable] = true, true
		result = append(result, Binding{d, p})
	}
	return result, nil
}
