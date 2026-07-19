package networkpolicy

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// allowedHostPattern 与 environments 写入校验共享同一份规则：
// 可选 `*.` 通配前缀、hostname 字符集、可选 `:port`。
var allowedHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

// ValidateAllowedHost 校验 allowed_hosts 条目。错误文案与 environments handler
// 既有合同保持一致，调用方直接透传。
func ValidateAllowedHost(host string) error {
	if strings.Contains(host, "://") || strings.Contains(host, "/") || !allowedHostPattern.MatchString(host) {
		return errors.New("config.networking.allowed_hosts entries must be hostnames without URL schemes")
	}
	if len(host) > 253 {
		return errors.New("config.networking.allowed_hosts entries must be at most 253 characters")
	}
	hostname, port := host, ""
	if separator := strings.LastIndexByte(host, ':'); separator >= 0 {
		hostname, port = host[:separator], host[separator+1:]
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errors.New("config.networking.allowed_hosts entries must be hostnames without URL schemes")
		}
	}
	hostname = strings.TrimPrefix(hostname, "*.")
	normalized, err := NormalizeHost(hostname)
	if err != nil || !validNormalizedHostname(normalized) {
		return errors.New("config.networking.allowed_hosts entries must be hostnames without URL schemes")
	}
	return nil
}

// NormalizeHost 对 allowlist 条目与 CONNECT target 做同一套归一化：
// 小写、去尾点、IDNA→punycode。
func NormalizeHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", errors.New("empty host")
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", err
	}
	return ascii, nil
}

func validNormalizedHostname(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

// matchAllowedEntry 判断单条 allowlist 条目是否命中已归一化的 target host。
// `*.example.com` 匹配任意深度子域但不含 apex；带非 443 端口的条目对
// 443-only 的 proxy 惰性，永不命中。
func matchAllowedEntry(entry string, normalizedTarget string) bool {
	host, port, err := net.SplitHostPort(entry)
	if err != nil {
		host, port = entry, ""
	}
	if port != "" && port != "443" {
		return false
	}
	if suffix, ok := strings.CutPrefix(host, "*."); ok {
		normalized, err := NormalizeHost(suffix)
		if err != nil {
			return false
		}
		return strings.HasSuffix(normalizedTarget, "."+normalized)
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return false
	}
	return normalized == normalizedTarget
}
