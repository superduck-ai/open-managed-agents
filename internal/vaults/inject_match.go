package vaults

import (
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// InjectionKind is the outbound proxy decision for a request URL.
type InjectionKind int

const (
	InjectionPassthrough InjectionKind = iota
	InjectionInject
	InjectionReject
)

// InjectionDecision is the result of matching vault credentials to an outbound URL.
type InjectionDecision struct {
	Kind       InjectionKind
	Credential *db.VaultCredential
}

// DecideInjection walks credentials in vault_ids order. First matching static_bearer
// wins; same-host coverage without an injectable path match rejects.
func DecideInjection(requestURL *url.URL, credentials []db.VaultCredential) InjectionDecision {
	if requestURL == nil {
		return InjectionDecision{Kind: InjectionReject}
	}
	hostCovered := false
	for i := range credentials {
		cred := &credentials[i]
		auth, err := decodeCredentialAuth(cred.Auth)
		if err != nil {
			continue
		}
		var rawServerURL string
		var authType credentialAuthType
		switch value := auth.value.(type) {
		case *mcpOAuthCredentialAuth:
			rawServerURL = value.MCPServerURL
			authType = value.Type
		case *staticBearerCredentialAuth:
			rawServerURL = value.MCPServerURL
			authType = value.Type
		default:
			continue
		}
		if rawServerURL == "" {
			continue
		}
		serverURL, err := url.Parse(rawServerURL)
		if err != nil || serverURL.Host == "" {
			continue
		}
		if !hostsEqual(serverURL, requestURL) {
			continue
		}
		hostCovered = true
		if !pathPrefixMatch(serverURL.Path, requestURL.Path) {
			continue
		}
		if !isInjectableStaticBearer(cred.AuthType, string(authType)) {
			continue
		}
		return InjectionDecision{Kind: InjectionInject, Credential: cred}
	}
	if hostCovered {
		return InjectionDecision{Kind: InjectionReject}
	}
	return InjectionDecision{Kind: InjectionPassthrough}
}

func hostsEqual(serverURL, requestURL *url.URL) bool {
	return strings.EqualFold(serverURL.Hostname(), requestURL.Hostname()) &&
		effectivePort(serverURL) == effectivePort(requestURL)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

// pathPrefixMatch treats mcp_server_url path as a '/' segment prefix of the request path.
func pathPrefixMatch(serverPath, requestPath string) bool {
	serverPath = normalizeURLPath(serverPath)
	requestPath = normalizeURLPath(requestPath)
	if serverPath == "/" {
		return true
	}
	return requestPath == serverPath || strings.HasPrefix(requestPath, serverPath+"/")
}

func normalizeURLPath(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "/"
	}
	return "/" + trimmed
}

func isInjectableStaticBearer(authType, cfgType string) bool {
	typ := strings.TrimSpace(authType)
	if typ == "" {
		typ = strings.TrimSpace(cfgType)
	}
	return typ == "static_bearer"
}
