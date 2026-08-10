package vaults

import (
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// injectionKind is the outbound proxy decision for a request URL.
type injectionKind int

const (
	injectionPassthrough injectionKind = iota
	injectionInject
	injectionReject
)

// injectionDecision is the result of matching vault credentials to an outbound URL.
type injectionDecision struct {
	Kind       injectionKind
	Credential *db.VaultCredential
}

// decideInjection walks credentials in vault_ids order. First matching injectable
// credential (static_bearer or mcp_oauth) wins; same scheme/host/port coverage
// without an injectable path match rejects. Unparseable auth fails closed.
func decideInjection(requestURL *url.URL, credentials []db.VaultCredential) injectionDecision {
	if requestURL == nil {
		return injectionDecision{Kind: injectionReject}
	}
	matches, hostCovered, err := listInjectableMatches(requestURL, credentials)
	if err != nil {
		return injectionDecision{Kind: injectionReject}
	}
	if len(matches) > 0 {
		return injectionDecision{Kind: injectionInject, Credential: matches[0]}
	}
	if hostCovered {
		return injectionDecision{Kind: injectionReject}
	}
	return injectionDecision{Kind: injectionPassthrough}
}

// listInjectableMatches returns vault_ids-ordered injectable credentials that
// match requestURL. hostCovered is true when any credential covers the host
// (including non-injectable types), for reject-vs-passthrough.
func listInjectableMatches(requestURL *url.URL, credentials []db.VaultCredential) ([]*db.VaultCredential, bool, error) {
	if requestURL == nil {
		return nil, false, nil
	}
	hostCovered := false
	matches := make([]*db.VaultCredential, 0)
	for i := range credentials {
		cred := &credentials[i]
		auth, err := decodeCredentialAuth(cred.Auth)
		if err != nil {
			return nil, false, err
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
		if !isInjectableCredential(cred.AuthType, string(authType)) {
			continue
		}
		matches = append(matches, cred)
	}
	return matches, hostCovered, nil
}

func hostsEqual(serverURL, requestURL *url.URL) bool {
	return strings.EqualFold(serverURL.Scheme, requestURL.Scheme) &&
		strings.EqualFold(serverURL.Hostname(), requestURL.Hostname()) &&
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

func isInjectableCredential(authType, cfgType string) bool {
	typ := strings.TrimSpace(authType)
	if typ == "" {
		typ = strings.TrimSpace(cfgType)
	}
	return typ == "static_bearer" || typ == "mcp_oauth"
}
