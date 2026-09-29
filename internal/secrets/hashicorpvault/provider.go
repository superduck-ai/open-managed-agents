// Package hashicorpvault wraps data keys using HashiCorp Vault Transit.
// Vault provisioning, authentication and token renewal are external to OMA.
package hashicorpvault

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const providerName = "hashicorp_vault"
const requestTimeout = 10 * time.Second
const maxResponseBytes = 64 * 1024

type Config struct {
	Address      string
	TransitMount string
	KeyName      string
	TokenFile    string
	Token        string
	CAFile       string
}

type Provider struct {
	baseURL   string
	keyName   string
	tokenFile string
	token     string
	client    *http.Client
}

func New(cfg Config) (*Provider, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.Address))
	if err != nil || !allowedOrigin(u) || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("hashicorp_vault: address must be HTTPS or local HTTP, without credentials, path, query or fragment")
	}
	mount, key := strings.TrimSpace(cfg.TransitMount), strings.TrimSpace(cfg.KeyName)
	if mount == "" {
		mount = "transit"
	}
	if !validMount(mount) || !validSegment(key) {
		return nil, errors.New("hashicorp_vault: transit_mount and key_name must be non-empty safe path segments")
	}
	token, tokenFile := strings.TrimSpace(cfg.Token), strings.TrimSpace(cfg.TokenFile)
	if (token == "") == (tokenFile == "") {
		return nil, errors.New("hashicorp_vault: configure exactly one of token or token_file")
	}
	if token != "" && !validToken([]byte(token)) {
		return nil, errors.New("hashicorp_vault: invalid token")
	}
	tlsConfig, err := clientTLSConfig(strings.TrimSpace(cfg.CAFile))
	if err != nil {
		return nil, err
	}
	proxy := http.ProxyFromEnvironment
	if u.Scheme == "http" {
		proxy = nil // Local tokens must not travel through an environment proxy.
	}
	return &Provider{
		baseURL: u.Scheme + "://" + u.Host + "/v1/" + mount,
		keyName: key, tokenFile: tokenFile, token: token,
		client: &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				Proxy:             proxy,
				TLSClientConfig:   tlsConfig,
				DialContext:       (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2: true, TLSHandshakeTimeout: 3 * time.Second,
				IdleConnTimeout: 90 * time.Second, MaxIdleConns: 10,
			},
		},
	}, nil
}

func allowedOrigin(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	// "vault" is the local Compose service; other remote hosts require HTTPS.
	return u.Scheme == "http" && (host == "localhost" || host == "vault" || net.ParseIP(host).IsLoopback())
}

func validMount(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if !validSegment(segment) {
			return false
		}
	}
	return true
}

func validSegment(value string) bool {
	return value != "" && value != "." && !strings.HasSuffix(value, ".") && !strings.ContainsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.')
	})
}

// Name is persisted in the envelope's key_provider field.
func (p *Provider) Name() string { return providerName }

// WrapDEK sends only the 32-byte DEK, never a business credential, to Transit.
func (p *Provider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	if len(dek) != 32 {
		return secrets.WrappedKey{}, errors.New("hashicorp_vault: DEK must be 32 bytes")
	}
	response, err := p.call(ctx, "encrypt", transitRequest{Plaintext: base64.StdEncoding.EncodeToString(dek)})
	if err != nil {
		return secrets.WrappedKey{}, err
	}
	if !validCiphertext(response.Ciphertext) {
		return secrets.WrappedKey{}, failure("encrypt", "InvalidResponse", 0)
	}
	// Vault's physical key version stays in the complete ciphertext string.
	// KeyVersion=1 identifies OMA's wrapping format, not the Transit key version.
	return secrets.WrappedKey{Ciphertext: []byte(response.Ciphertext), KeyVersion: 1}, nil
}

// UnwrapDEK returns an owned buffer, which secrets.Service clears after use.
func (p *Provider) UnwrapDEK(ctx context.Context, wrapped secrets.WrappedKey) ([]byte, error) {
	if wrapped.KeyVersion != 1 || !validCiphertext(string(wrapped.Ciphertext)) {
		return nil, errors.New("hashicorp_vault: invalid wrapped DEK metadata")
	}
	response, err := p.call(ctx, "decrypt", transitRequest{Ciphertext: string(wrapped.Ciphertext)})
	if err != nil {
		return nil, err
	}
	dek, err := base64.StdEncoding.DecodeString(response.Plaintext)
	if err != nil || len(dek) != 32 {
		clear(dek)
		return nil, failure("decrypt", "InvalidResponse", 0)
	}
	return dek, nil
}

func validCiphertext(value string) bool {
	if len(value) > maxResponseBytes || !strings.HasPrefix(value, "vault:v") {
		return false
	}
	version, encoded, ok := strings.Cut(strings.TrimPrefix(value, "vault:v"), ":")
	if !ok || version == "" || version[0] < '1' || version[0] > '9' {
		return false
	}
	n, err := strconv.ParseUint(version, 10, 64)
	if err != nil || n == 0 {
		return false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	return err == nil && len(data) > 0
}
