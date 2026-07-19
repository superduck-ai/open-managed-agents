// Package networkpolicy 是 Environment networking（unrestricted/limited）的纯策略模块。
// 它不访问数据库：调用方组装 Subject，模块返回带机器可测 reason 的 Decision。
// Code Session upstream proxy 是主要执行点；E2B Adapter 复用同一解析与 host catalog。
package networkpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrMalformedConfig 表示 Environment 配置不是合法 JSON。
var ErrMalformedConfig = errors.New("malformed environment config")

// ErrUnknownNetworkingType 表示 networking.type 不是已知值，必须 fail closed。
var ErrUnknownNetworkingType = errors.New("unknown networking type")

// Type 是 Environment networking 模式。
type Type string

const (
	TypeUnrestricted Type = "unrestricted"
	TypeLimited      Type = "limited"
)

// Config 是 Environment networking 的领域类型。
type Config struct {
	Type                 Type
	AllowedHosts         []string
	AllowMCPServers      bool
	AllowPackageManagers bool
}

// ParseConfig 解析完整 Environment 配置 JSON 并提取 networking。
// 顶层 type 不是 "cloud"（含缺失）时视为 unrestricted——非 cloud Environment
// 没有受管 Sandbox 出口；networking 缺失或为 null 时同样视为 unrestricted。
// networking 对象存在但 type 未知（含空串）、或 JSON 畸形时返回错误，
// 调用方必须 fail closed，绝不降级为 unrestricted。
func ParseConfig(raw json.RawMessage) (Config, error) {
	if len(raw) == 0 {
		return Config{Type: TypeUnrestricted}, nil
	}
	var config struct {
		Type       string          `json:"type"`
		Networking json.RawMessage `json:"networking"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrMalformedConfig, err)
	}
	if config.Type != "cloud" {
		return Config{Type: TypeUnrestricted}, nil
	}
	if len(config.Networking) == 0 || string(config.Networking) == "null" {
		return Config{Type: TypeUnrestricted}, nil
	}
	var networking struct {
		Type                 string   `json:"type"`
		AllowedHosts         []string `json:"allowed_hosts"`
		AllowMCPServers      bool     `json:"allow_mcp_servers"`
		AllowPackageManagers bool     `json:"allow_package_managers"`
	}
	if err := json.Unmarshal(config.Networking, &networking); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrMalformedConfig, err)
	}
	switch networking.Type {
	case string(TypeUnrestricted):
		return Config{Type: TypeUnrestricted}, nil
	case string(TypeLimited):
		if err := validateConfigAllowedHosts(networking.AllowedHosts); err != nil {
			return Config{}, err
		}
		return Config{
			Type:                 TypeLimited,
			AllowedHosts:         networking.AllowedHosts,
			AllowMCPServers:      networking.AllowMCPServers,
			AllowPackageManagers: networking.AllowPackageManagers,
		}, nil
	default:
		return Config{}, fmt.Errorf("%w: %q", ErrUnknownNetworkingType, networking.Type)
	}
}

func validateConfigAllowedHosts(hosts []string) error {
	for _, host := range hosts {
		if err := ValidateAllowedHost(host); err != nil {
			return fmt.Errorf("%w: invalid allowed_hosts entry: %v", ErrMalformedConfig, err)
		}
	}
	return nil
}
