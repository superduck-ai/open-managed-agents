package mcpcatalogs

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrInvalidEndpoint = errors.New("invalid MCP endpoint")

const maxEndpointURLBytes = 2048

// NormalizeEndpoint 校验并规范化匿名 MCP HTTP endpoint，使语义等价的 URL 可以共享同一条全局 catalog。
// 返回值会直接参与数据库唯一键；函数只处理 URL 结构，不执行 DNS 解析或目标地址网络策略检查。
func NormalizeEndpoint(raw string) (string, error) {
	// 先去除配置值两端的空白，再要求 URL 同时具有明确的 scheme 和 host。
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ErrInvalidEndpoint
	}

	// MCP URL transport 当前只支持 HTTP(S)；scheme 大小写不应影响 endpoint identity。
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidEndpoint
	}

	// 全局 catalog 仅表示匿名探测。拒绝 userinfo、query 和 fragment，避免把凭据或语义不同的
	// 请求错误地归并到同一匿名 endpoint；这里选择拒绝而不是静默删除这些部分。
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", ErrInvalidEndpoint
	}

	// DNS hostname 不区分大小写，末尾的根域点也不改变目标；统一二者以减少重复 catalog。
	// 同时拒绝不可作为合法 hostname 一部分的控制字符和空白字符。
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" || strings.ContainsAny(hostname, "\x00\r\n\t ") {
		return "", ErrInvalidEndpoint
	}

	// 验证显式端口，并移除与 scheme 对应的默认端口，使 :80/:443 与省略端口得到相同 identity。
	port := parsed.Port()
	if port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", ErrInvalidEndpoint
		}
	}
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}

	// url.Hostname 会移除 IPv6 方括号；写回 host 时补回标准 URL 形式，并安全拼接可选端口。
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(strings.Trim(hostname, "[]"), port)
	}

	// 空 path 与根路径对 HTTP 请求等价，统一为 "/"，避免同一 endpoint 产生两条记录。
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	normalized := parsed.String()

	// endpoint_url 直接参与全局唯一索引，因此限制字节长度，避免异常长 URL 超过数据库索引上限。
	if len(normalized) > maxEndpointURLBytes {
		return "", ErrInvalidEndpoint
	}
	return normalized, nil
}
