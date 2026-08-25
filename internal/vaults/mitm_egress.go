package vaults

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// EgressSession identifies the Code Session tenant for MITM outbound rewriting.
type EgressSession struct {
	CodeSessionExternalID string
	OrganizationUUID      string
	WorkspaceUUID         string
}

// MITMEgress is the deep module for Managed Agent CONNECT MITM outbound
// rewriting. Callers only use Prepare: env placeholder substitution, Git Smart
// HTTP Authorization, then MCP Authorization inject.
type MITMEgress struct {
	env *EgressSubstitutor
	inj *Injector
}

// NewMITMEgress wires MITM outbound rewriting. database/secretSvc drive env +
// Git stages; inj may be shared with Session MCP HTTP proxy. Nil inj skips MCP.
// Returns nil when neither env/Git nor MCP can run.
func NewMITMEgress(
	database *db.DB,
	secretSvc *secrets.Service,
	logger *slog.Logger,
	inj *Injector,
) *MITMEgress {
	var store credentialStore
	if database != nil {
		store = database
	}
	var env *EgressSubstitutor
	if store != nil && secretSvc != nil {
		env = newEgressSubstitutor(store, secretSvc, logging.LoggerOrDefault(logger))
	}
	if env == nil && inj == nil {
		return nil
	}
	return &MITMEgress{env: env, inj: inj}
}

// newMITMEgressForTest builds MITMEgress from package-local doubles.
func newMITMEgressForTest(env *EgressSubstitutor, inj *Injector) *MITMEgress {
	if env == nil && inj == nil {
		return nil
	}
	return &MITMEgress{env: env, inj: inj}
}

// Prepare mutates req for env substitution and Git Smart HTTP Authorization,
// then returns a RoundTripper that performs MCP Authorization inject on base.
//
// Ordering is fixed: env substitute → Git Basic → MCP inject wrap.
// Credentials are loaded once for env and Git. Absolute URL for MCP credential
// match is built from CONNECT authority + origin-form path/query.
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
	if err := e.rewriteOutbound(ctx, session, host, port, req); err != nil {
		return nil, err
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

func (e *MITMEgress) rewriteOutbound(
	ctx context.Context,
	session EgressSession,
	host string,
	port string,
	req *http.Request,
) error {
	if e.env == nil {
		return nil
	}
	credentials, err := e.env.loadCredentials(
		ctx,
		session.CodeSessionExternalID,
		session.OrganizationUUID,
		session.WorkspaceUUID,
	)
	if err != nil {
		return err
	}
	if len(credentials) == 0 {
		return nil
	}
	opened := make(map[string]string)
	if err := e.env.applyEnvSubstitutions(ctx, req, host, port, credentials, opened); err != nil {
		return err
	}
	return authorizeGitSmartHTTP(ctx, e.env, req, host, port, credentials, opened)
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
