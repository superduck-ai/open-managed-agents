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
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("url\x00"))
	_, _ = mac.Write([]byte(normalizedURL))
	return "mcpe_" + hex.EncodeToString(mac.Sum(nil))
}
