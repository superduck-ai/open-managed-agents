// Package aliyunkms is a leaf KeyProvider plugin. It sends only 32-byte DEKs
// to Alibaba Cloud KMS; credential payloads and database models never enter it.
package aliyunkms

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	kms "github.com/alibabacloud-go/kms-20160120/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/credentials-go/credentials"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const providerName = "aliyun_kms"
const requestTimeout = 10 * time.Second

// KMS encodes its physical CMK version in CiphertextBlob. The existing integer
// key_version column identifies our wrapping contract, not that opaque version.
const wrappingVersion int64 = 1

// Config contains inputs for the SDK. Endpoint is required, with optional https://.
type Config struct {
	Endpoint        string
	KeyID           string
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	// CA is an optional PEM trust bundle for an explicitly configured client.
	CA string
}

type client interface {
	EncryptWithContext(context.Context, *kms.EncryptRequest, *dara.RuntimeOptions) (*kms.EncryptResponse, error)
	DecryptWithContext(context.Context, *kms.DecryptRequest, *dara.RuntimeOptions) (*kms.DecryptResponse, error)
}

// Provider implements secrets.KeyProvider without retaining plaintext DEKs.
type Provider struct {
	client         client
	keyID          string
	canonicalKeyID string
}

// New validates configuration and creates a reusable SDK client without network I/O.
func New(cfg Config) (*Provider, error) {
	if initialSDKWireLogging || sdkWireLogging(os.Getenv("DEBUG")) {
		return nil, errors.New("aliyun_kms: remove dara, tea and credential from DEBUG to prevent SDK wire logging")
	}
	endpoint, err := endpointHost(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	keyID := strings.TrimSpace(cfg.KeyID)
	canonical, err := canonicalKeyID(keyID)
	if err != nil {
		return nil, err
	}
	credential, err := newCredential(cfg)
	if err != nil {
		return nil, err
	}
	sdk, err := kms.NewClient(&openapi.Config{
		Endpoint: dara.String(endpoint), Protocol: dara.String("https"),
		Credential: credential, ConnectTimeout: dara.Int(3000), ReadTimeout: dara.Int(10000),
		HttpClient: &httpClient{}, Ca: dara.String(cfg.CA),
	})
	if err != nil {
		return nil, errors.New("aliyun_kms: initialize SDK client failed")
	}
	return &Provider{client: sdk, keyID: keyID, canonicalKeyID: canonical}, nil
}

// SDK loggers capture DEBUG during package initialization. Retain that state
// even if application configuration later changes the environment.
var initialSDKWireLogging = sdkWireLogging(os.Getenv("DEBUG"))

func sdkWireLogging(value string) bool {
	for _, flag := range strings.Split(value, ",") {
		switch flag {
		case "dara", "tea", "credential":
			return true
		}
	}
	return false
}

func endpointHost(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", errors.New("aliyun_kms: endpoint must be an HTTPS host without credentials, path, query or fragment")
	}
	return u.Host, nil
}

func canonicalKeyID(keyID string) (string, error) {
	if strings.HasPrefix(keyID, "acs:") {
		parts := strings.Split(keyID, ":")
		if len(parts) != 5 || parts[1] != "kms" || parts[2] == "" || parts[3] == "" || !strings.HasPrefix(parts[4], "key/") {
			return "", errors.New("aliyun_kms: invalid key ARN")
		}
		keyID = strings.TrimPrefix(parts[4], "key/")
	}
	if keyID == "" || strings.ContainsAny(keyID, "/: \t\r\n") {
		return "", errors.New("aliyun_kms: key_id must be a CMK ID or ARN")
	}
	return keyID, nil
}

func newCredential(cfg Config) (credentials.Credential, error) {
	if (cfg.AccessKeyID == "") != (cfg.AccessKeySecret == "") || (cfg.SecurityToken != "" && cfg.AccessKeyID == "") {
		return nil, errors.New("aliyun_kms: access key credentials are incomplete")
	}
	// Select workload identity explicitly: a partially configured OIDC identity
	// must fail rather than fall through to a different role or a local profile.
	credentialConfig := &credentials.Config{Type: dara.String("ecs_ram_role"), DisableIMDSv1: dara.Bool(true)}
	if os.Getenv("ALIBABA_CLOUD_OIDC_TOKEN_FILE") != "" || os.Getenv("ALIBABA_CLOUD_OIDC_PROVIDER_ARN") != "" || os.Getenv("ALIBABA_CLOUD_ROLE_ARN") != "" {
		credentialConfig = &credentials.Config{Type: dara.String("oidc_role_arn"), ConnectTimeout: dara.Int(3000), Timeout: dara.Int(10000)}
	}
	if cfg.AccessKeyID != "" {
		credentialConfig = &credentials.Config{Type: dara.String("access_key"), AccessKeyId: &cfg.AccessKeyID, AccessKeySecret: &cfg.AccessKeySecret}
		if cfg.SecurityToken != "" {
			credentialConfig.Type = dara.String("sts")
			credentialConfig.SecurityToken = &cfg.SecurityToken
		}
	}
	credential, err := credentials.NewCredential(credentialConfig)
	if err != nil {
		return nil, errors.New("aliyun_kms: initialize credentials failed")
	}
	return credential, nil
}

// Name is persisted in the envelope's existing key_provider column.
func (p *Provider) Name() string { return providerName }

func (p *Provider) encryptionContext() map[string]any {
	return map[string]any{"purpose": "oma-dek-v1", "key_id": p.canonicalKeyID}
}

// WrapDEK encrypts exactly one AES-256 data key, never a credential payload.
func (p *Provider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	if len(dek) != 32 {
		return secrets.WrappedKey{}, errors.New("aliyun_kms: DEK must be 32 bytes")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request := &kms.EncryptRequest{KeyId: &p.keyID, Plaintext: dara.String(base64.StdEncoding.EncodeToString(dek)), EncryptionContext: p.encryptionContext()}
	response, err := p.client.EncryptWithContext(ctx, request, runtimeOptions())
	if err != nil {
		return secrets.WrappedKey{}, operationError(ctx, "encrypt", err)
	}
	if response == nil || response.Body == nil || dara.StringValue(response.Body.KeyId) != p.canonicalKeyID {
		return secrets.WrappedKey{}, errors.New("aliyun_kms: encrypt returned an unexpected key")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(dara.StringValue(response.Body.CiphertextBlob))
	if err != nil || len(ciphertext) == 0 {
		return secrets.WrappedKey{}, errors.New("aliyun_kms: encrypt returned invalid ciphertext")
	}
	return secrets.WrappedKey{Ciphertext: ciphertext, KeyVersion: wrappingVersion}, nil
}

// UnwrapDEK pins the response to the configured CMK and returns an owned DEK
// buffer. The secrets.Service caller must clear it on every exit path.
func (p *Provider) UnwrapDEK(ctx context.Context, wrapped secrets.WrappedKey) ([]byte, error) {
	if wrapped.KeyVersion != wrappingVersion || len(wrapped.Ciphertext) == 0 {
		return nil, errors.New("aliyun_kms: invalid wrapped DEK metadata")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	response, err := p.client.DecryptWithContext(ctx, &kms.DecryptRequest{
		CiphertextBlob: dara.String(base64.StdEncoding.EncodeToString(wrapped.Ciphertext)), EncryptionContext: p.encryptionContext(),
	}, runtimeOptions())
	if err != nil {
		return nil, operationError(ctx, "decrypt", err)
	}
	if response == nil || response.Body == nil || dara.StringValue(response.Body.KeyId) != p.canonicalKeyID {
		return nil, errors.New("aliyun_kms: decrypt returned an unexpected key")
	}
	dek, err := base64.StdEncoding.DecodeString(dara.StringValue(response.Body.Plaintext))
	if err != nil || len(dek) != 32 {
		clear(dek)
		return nil, errors.New("aliyun_kms: decrypt returned an invalid DEK")
	}
	return dek, nil
}

func runtimeOptions() *dara.RuntimeOptions {
	return &dara.RuntimeOptions{Autoretry: dara.Bool(false), MaxAttempts: dara.Int(1)}
}

// The SDK delegates HTTP here so redirects cannot forward signed DEKs to a
// different endpoint. Preserve the transport supplied by the SDK.
type httpClient struct{}

func (*httpClient) Call(request *http.Request, transport *http.Transport) (*http.Response, error) {
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client.Do(request)
}
