package vaults

import (
	"context"
	"net"
	"net/http"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// EgressSession identifies the Code Session tenant for MITM outbound rewriting.
type EgressSession struct {
	CodeSessionExternalID string
	OrganizationUUID      string
	WorkspaceUUID         string
}

// MITMEgress is the deep module for Managed Agent CONNECT MITM outbound
// rewriting: environment_variable substitution then MCP Authorization inject.
type MITMEgress struct {
	sub *EgressSubstitutor
	inj *Injector
}

// NewMITMEgress wires env substitution and MCP injection for MITM. Either
// dependency may be nil; nil means that stage is skipped.
func NewMITMEgress(sub *EgressSubstitutor, inj *Injector) *MITMEgress {
	if sub == nil && inj == nil {
		return nil
	}
	return &MITMEgress{sub: sub, inj: inj}
}

// Prepare mutates req for env substitution, then returns a RoundTripper that
// performs MCP Authorization inject (including mcp_oauth 401 refresh) on base.
//
// Ordering is fixed: env substitute → MCP inject wrap.
// Absolute URL for credential match is built from CONNECT authority + origin-form path/query.
// When an injectable credential matches, Injector overwrites Authorization with the vault Bearer.
func (e *MITMEgress) Prepare(
	ctx context.Context,
	session EgressSession,
	connectAuthority string,
	req *http.Request,
	base http.RoundTripper,
) (http.RoundTripper, error) {
	if e == nil {
		return base, nil
	}
	host, port := splitConnectAuthority(connectAuthority)
	if e.sub != nil {
		if err := e.sub.SubstituteEnvSecrets(
			ctx,
			session.CodeSessionExternalID,
			session.OrganizationUUID,
			session.WorkspaceUUID,
			req,
			host,
			port,
		); err != nil {
			return nil, err
		}
	}
	if e.inj == nil {
		return base, nil
	}
	abs := absoluteMITMRequestURL(connectAuthority, req)
	return e.inj.WrapTransport(
		ctx,
		session.CodeSessionExternalID,
		session.OrganizationUUID,
		session.WorkspaceUUID,
		abs,
		base,
	), nil
}

func splitConnectAuthority(authority string) (host, port string) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		return authority, "443"
	}
	if port == "" {
		port = "443"
	}
	return host, port
}

func absoluteMITMRequestURL(connectAuthority string, req *http.Request) *url.URL {
	path := "/"
	rawPath := ""
	rawQuery := ""
	if req != nil && req.URL != nil {
		if req.URL.Path != "" {
			path = req.URL.Path
		}
		rawPath = req.URL.RawPath
		rawQuery = req.URL.RawQuery
	}
	return &url.URL{
		Scheme:   "https",
		Host:     connectAuthority,
		Path:     path,
		RawPath:  rawPath,
		RawQuery: rawQuery,
	}
}

// hasInjectableMCPCredential reports whether any credential is static_bearer or mcp_oauth.
func hasInjectableMCPCredential(credentials []db.VaultCredential) bool {
	for i := range credentials {
		switch credentialAuthType(credentials[i].AuthType) {
		case credentialAuthTypeStaticBearer, credentialAuthTypeMCPOAuth:
			return true
		}
	}
	return false
}
