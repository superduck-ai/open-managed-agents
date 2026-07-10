package mcpcatalogs

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrInvalidEndpoint = errors.New("invalid MCP endpoint")

const maxEndpointURLBytes = 2048

func NormalizeEndpoint(raw string) (string, error) {
	// catalog identity 基于规范化 URL；这里统一大小写、默认端口和空路径，
	// 保证语义等价的 endpoint 不会生成多份缓存和发现任务。
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ErrInvalidEndpoint
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidEndpoint
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", ErrInvalidEndpoint
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" || strings.ContainsAny(hostname, "\x00\r\n\t ") {
		return "", ErrInvalidEndpoint
	}
	port := parsed.Port()
	if port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", ErrInvalidEndpoint
		}
	}
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(strings.Trim(hostname, "[]"), port)
	}
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
