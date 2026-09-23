package vaults

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const gitSmartHTTPBasicUsername = "oauth2"

// gitSmartHTTPRepositoryPath preserves escaped paths to prevent encoded endpoints from falling back to Vault.
func gitSmartHTTPRepositoryPath(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	path := strings.TrimRight(req.URL.EscapedPath(), "/")
	if req.Method == http.MethodPost {
		for _, suffix := range []string{"/git-upload-pack", "/git-receive-pack"} {
			if repo, ok := strings.CutSuffix(path, suffix); ok {
				return repo
			}
		}
	}
	if req.Method == http.MethodGet {
		service := req.URL.Query().Get("service")
		if service == "git-upload-pack" || service == "git-receive-pack" {
			if repo, ok := strings.CutSuffix(path, "/info/refs"); ok {
				return repo
			}
		}
	}
	return ""
}

func setGitSmartHTTPAuthorization(header http.Header, secret string) {
	userinfo := gitSmartHTTPBasicUsername + ":" + secret
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(userinfo)))
}

func authorizeGitSmartHTTP(
	ctx context.Context,
	opener envSecretOpener,
	req *http.Request,
	host string,
	port string,
	credentials []db.VaultCredential,
	opened map[string]string,
) error {
	if gitSmartHTTPRepositoryPath(req) == "" {
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
