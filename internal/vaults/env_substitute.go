package vaults

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// egressSubstitution is one Opaque Placeholder → secret replacement scoped to
// Injection Location. Host matching is resolved before this value is built.
type egressSubstitution struct {
	placeholder string
	secretValue string
	header      bool
	body        bool
}

// applyEgressSubstitutions rewrites an already snapshotted request. Matching
// and replacement use the same bounded body bytes.
func applyEgressSubstitutions(req *http.Request, body []byte, substitutions []egressSubstitution) {
	if len(substitutions) == 0 {
		return
	}
	for _, rule := range substitutions {
		if rule.header {
			for key, values := range req.Header {
				for i, value := range values {
					if strings.Contains(value, rule.placeholder) {
						req.Header[key][i] = strings.ReplaceAll(value, rule.placeholder, rule.secretValue)
					}
				}
			}
		}
		if rule.body && strings.Contains(string(body), rule.placeholder) {
			body = []byte(strings.ReplaceAll(string(body), rule.placeholder, rule.secretValue))
		}
	}
	if len(body) > 0 {
		restoreRequestBody(req, body)
	}
}

// requestNeedsPlaceholder reports whether placeholder appears in locations the
// credential enables. Used so Open is only attempted when substitution would apply.
func requestNeedsPlaceholder(req *http.Request, body []byte, placeholder string, location credentialInjectionLocation) bool {
	if location.Header {
		for _, values := range req.Header {
			for _, value := range values {
				if strings.Contains(value, placeholder) {
					return true
				}
			}
		}
	}
	return location.Body && strings.Contains(string(body), placeholder)
}

// EgressSubstitutor loads session vault credentials per outbound MITM request
// and performs Egress Secret Substitution plus Git Smart HTTP Authorization.
// Plaintext secrets are never cached.
type EgressSubstitutor struct {
	store     credentialStore
	secretSvc *secrets.Service
	logger    *slog.Logger
}

func NewEgressSubstitutor(database *db.DB, secretSvc *secrets.Service, logger *slog.Logger) *EgressSubstitutor {
	var store credentialStore
	if database != nil {
		store = database
	}
	return newEgressSubstitutor(store, secretSvc, logger)
}

func newEgressSubstitutor(store credentialStore, secretSvc *secrets.Service, logger *slog.Logger) *EgressSubstitutor {
	return &EgressSubstitutor{
		store:     store,
		secretSvc: secretSvc,
		logger:    logging.LoggerOrDefault(logger),
	}
}

// SubstituteEnvSecrets rewrites Opaque Placeholders in the outbound request for
// Environment Variable Credentials attached to the code session, then applies
// Git Smart HTTP Authorization when the request is Git Smart HTTP.
func (s *EgressSubstitutor) SubstituteEnvSecrets(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
	req *http.Request,
	targetHost string,
	targetPort string,
) error {
	host := strings.TrimSpace(targetHost)
	port := strings.TrimSpace(targetPort)
	if port == "" {
		port = "443"
	}
	if s == nil || s.store == nil {
		return substitutionRejected(errCredentialStoreUnavailable)
	}
	vaultIDs, err := s.store.GetCodeSessionVaultIDs(ctx, codeSessionExternalID, organizationUUID, workspaceUUID)
	if err != nil {
		return substitutionRejected(err)
	}
	if len(vaultIDs) == 0 {
		return nil
	}
	credentials, err := s.store.ListActiveVaultCredentialsForVaultIDs(ctx, workspaceUUID, vaultIDs)
	if err != nil {
		return substitutionRejected(err)
	}
	plan, err := s.planEnvEgress(ctx, req, host, port, credentials)
	if err != nil {
		return err
	}
	applyEgressSubstitutions(req, plan.body, plan.substitutions)
	if plan.gitSecret != "" {
		setGitSmartHTTPAuthorization(req.Header, plan.gitSecret)
	}
	return nil
}

type envEgressPlan struct {
	body          []byte
	substitutions []egressSubstitution
	gitSecret     string
}

func (s *EgressSubstitutor) planEnvEgress(
	ctx context.Context,
	req *http.Request,
	host string,
	port string,
	credentials []db.VaultCredential,
) (envEgressPlan, error) {
	_, bound, err := uniqueEnvironmentCredentials(credentials)
	if err != nil {
		return envEgressPlan{}, substitutionRejected(err)
	}
	opened := make(map[string]string)
	var gitSecret string
	if isGitSmartHTTPRequest(req) {
		gitCred, err := firstGitAuthorizationCredential(credentials, host, port)
		if err != nil {
			return envEgressPlan{}, substitutionRejected(err)
		}
		if gitCred != nil {
			secret, err := s.openBoundSecret(ctx, opened, *gitCred)
			if err != nil {
				return envEgressPlan{}, err
			}
			gitSecret = secret
		}
	}
	out := make([]egressSubstitution, 0)
	var body []byte
	bodyLoaded := false
	for _, item := range bound {
		covers, err := credentialNetworkingCoversHost(item.value.Networking, host, port)
		if err != nil {
			return envEgressPlan{}, substitutionRejected(err)
		}
		if !covers {
			continue
		}
		if item.value.InjectionLocation.Body && !bodyLoaded {
			body, err = snapshotRequestBody(req)
			if err != nil {
				return envEgressPlan{}, substitutionRejected(err)
			}
			bodyLoaded = true
		}
		if !requestNeedsPlaceholder(req, body, item.value.Placeholder, item.value.InjectionLocation) {
			continue
		}
		secret, err := s.openBoundSecret(ctx, opened, item)
		if err != nil {
			return envEgressPlan{}, err
		}
		out = append(out, egressSubstitution{
			placeholder: item.value.Placeholder,
			secretValue: secret,
			header:      item.value.InjectionLocation.Header,
			body:        item.value.InjectionLocation.Body,
		})
	}
	return envEgressPlan{body: body, substitutions: out, gitSecret: gitSecret}, nil
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

func (s *EgressSubstitutor) openBoundSecret(ctx context.Context, opened map[string]string, item environmentCredential) (string, error) {
	if secret, ok := opened[item.row.ExternalID]; ok {
		return secret, nil
	}
	secret, err := s.openEnvironmentSecret(ctx, &item.row)
	if err != nil {
		s.logger.WarnContext(ctx, "open environment variable credential failed",
			"credential_id", item.row.ExternalID,
			"auth_type", item.row.AuthType,
			"error", err,
		)
		return "", substitutionRejected(err)
	}
	opened[item.row.ExternalID] = secret
	return secret, nil
}

// credentialNetworkingCoversHost reports whether Credential Networking allows
// Egress Secret Substitution or Git Smart HTTP Authorization for host:port.
func credentialNetworkingCoversHost(networking credentialAuthNetworking, host, port string) (bool, error) {
	if networking.Type == "unrestricted" {
		return true, nil
	}
	if networking.Type != "limited" || networking.AllowedHosts == nil {
		return false, nil
	}
	return networkpolicy.AllowsHost(*networking.AllowedHosts, host, port)
}

func (s *EgressSubstitutor) openEnvironmentSecret(ctx context.Context, credential *db.VaultCredential) (string, error) {
	plaintext, err := openCredentialSecret(ctx, s.secretSvc, *credential)
	if err != nil {
		return "", err
	}
	defer clear(plaintext)

	secret, err := decodeEnvironmentVariableCredentialSecret(plaintext)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secret.SecretValue) == "" {
		return "", errors.New("environment_variable secret_value is empty")
	}
	return secret.SecretValue, nil
}
