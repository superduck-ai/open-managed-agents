package vaults

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const gitSmartHTTPBasicUsername = "oauth2"

// isGitSmartHTTPRequest reports whether req is a valid Git Smart HTTP transfer:
// GET …/info/refs?service=git-upload-pack|git-receive-pack, or
// POST …/git-upload-pack|git-receive-pack.
// Git LFS, dumb HTTP, wrong methods, and Git REST APIs are not.
func isGitSmartHTTPRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	path := strings.TrimRight(req.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/git-upload-pack"), strings.HasSuffix(path, "/git-receive-pack"):
		return req.Method == http.MethodPost
	case strings.HasSuffix(path, "/info/refs"):
		if req.Method != http.MethodGet {
			return false
		}
		service := req.URL.Query().Get("service")
		return service == "git-upload-pack" || service == "git-receive-pack"
	default:
		return false
	}
}

func setGitSmartHTTPAuthorization(header http.Header, secret string) {
	userinfo := gitSmartHTTPBasicUsername + ":" + secret
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(userinfo)))
}

// authorizeGitSmartHTTP writes Basic oauth2:<secret> when the request is Git
// Smart HTTP and an environment_variable credential covers the CONNECT host
// with header Injection Location. Uncovered hosts and non-Git paths passthrough.
// Open failure is fail-closed (ErrSubstitutionRejected).
func authorizeGitSmartHTTP(
	ctx context.Context,
	opener envSecretOpener,
	req *http.Request,
	host string,
	port string,
	credentials []db.VaultCredential,
	opened map[string]string,
) error {
	if !isGitSmartHTTPRequest(req) {
		return nil
	}
	cred, err := firstGitAuthorizationCredential(credentials, host, port)
	if err != nil {
		return substitutionRejected(err)
	}
	if cred == nil {
		return nil
	}
	secret, err := opener.openBoundSecret(ctx, opened, *cred)
	if err != nil {
		return err
	}
	setGitSmartHTTPAuthorization(req.Header, secret)
	return nil
}

func firstGitAuthorizationCredential(credentials []db.VaultCredential, host, port string) (*environmentCredential, error) {
	for i := range credentials {
		if credentialAuthType(credentials[i].AuthType) != credentialAuthTypeEnvironmentVariable {
			continue
		}
		value, err := decodeEnvironmentCredentialAuth(credentials[i].Auth)
		if err != nil {
			return nil, err
		}
		if PlatformReservedSecretName(value.SecretName) || !value.InjectionLocation.Header {
			continue
		}
		covers, err := credentialNetworkingCoversHost(value.Networking, host, port)
		if err != nil {
			return nil, err
		}
		if !covers {
			continue
		}
		return &environmentCredential{row: credentials[i], value: value}, nil
	}
	return nil, nil
}
