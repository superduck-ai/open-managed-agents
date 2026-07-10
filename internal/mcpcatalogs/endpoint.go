package mcpcatalogs

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrInvalidEndpoint = errors.New("invalid MCP endpoint")

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
	return parsed.String(), nil
}

func EndpointKey(secret, normalizedURL string) string {
	// EndpointKey 同时用于 catalog 去重和接口中的稳定指纹；HMAC 避免直接暴露原始 URL。
	// 更换 secret 会改变 catalog identity，运维上应将其视为一次数据迁移。
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("url\x00"))
	_, _ = mac.Write([]byte(normalizedURL))
	return "mcpe_" + hex.EncodeToString(mac.Sum(nil))
}
