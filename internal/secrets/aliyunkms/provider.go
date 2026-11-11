// Package aliyunkms is a leaf KeyProvider plugin. It sends only 32-byte DEKs
// to Alibaba Cloud KMS; credential payloads and database models never enter it.
package aliyunkms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"math/rand/v2"
	"net"
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
const attemptPhaseTimeout = 3 * time.Second
const minimumAttemptBudget = time.Second
const idleConnectionTimeout = 30 * time.Second
const maxIdleConnectionsPerHost = 4

var defaultRetryDelays = []time.Duration{100 * time.Millisecond, 400 * time.Millisecond}

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
	retryDelays    []time.Duration
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
	transport, err := newHTTPClient(cfg.CA)
	if err != nil {
		return nil, err
	}
	sdk, err := kms.NewClient(&openapi.Config{
		Endpoint: dara.String(endpoint), Protocol: dara.String("https"),
		Credential: credential, ConnectTimeout: dara.Int(3000), ReadTimeout: dara.Int(10000),
		HttpClient: transport,
	})
	if err != nil {
		return nil, errors.New("aliyun_kms: initialize SDK client failed")
	}
	return &Provider{client: sdk, keyID: keyID, canonicalKeyID: canonical, retryDelays: defaultRetryDelays}, nil
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
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
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
	return &synchronizedCredential{Credential: credential}, nil
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
	request := &kms.EncryptRequest{KeyId: &p.keyID, Plaintext: dara.String(base64.StdEncoding.EncodeToString(dek)), EncryptionContext: p.encryptionContext()}
	var response *kms.EncryptResponse
	err := p.invoke(ctx, "encrypt", func(ctx context.Context) error {
		var err error
		response, err = p.client.EncryptWithContext(ctx, request, runtimeOptions())
		return err
	})
	if err != nil {
		return secrets.WrappedKey{}, err
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
	request := &kms.DecryptRequest{
		CiphertextBlob: dara.String(base64.StdEncoding.EncodeToString(wrapped.Ciphertext)), EncryptionContext: p.encryptionContext(),
	}
	var response *kms.DecryptResponse
	err := p.invoke(ctx, "decrypt", func(ctx context.Context) error {
		var err error
		response, err = p.client.DecryptWithContext(ctx, request, runtimeOptions())
		return err
	})
	if err != nil {
		return nil, err
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

func (p *Provider) invoke(ctx context.Context, operation string, call func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		err := call(ctx)
		if err == nil {
			return nil
		}
		delay, retry := p.retryDelay(ctx, err, attempt)
		if !retry || !waitForRetry(ctx, delay) {
			return operationError(ctx, operation, err, attempt)
		}
	}
}

func (p *Provider) retryDelay(ctx context.Context, err error, attempt int) (time.Duration, bool) {
	if attempt > len(p.retryDelays) || ctx.Err() != nil {
		return 0, false
	}
	if _, retryable := classifyFailure(err); !retryable {
		return 0, false
	}
	delay := jitter(p.retryDelays[attempt-1])
	deadline, _ := ctx.Deadline()
	return delay, time.Until(deadline) >= delay+minimumAttemptBudget
}

func jitter(delay time.Duration) time.Duration {
	return delay + time.Duration((rand.Float64()*0.4-0.2)*float64(delay))
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type httpClient struct {
	client *http.Client
}

func newHTTPClient(ca string) (*httpClient, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(ca)) {
			return nil, errors.New("aliyun_kms: ca must contain a PEM certificate")
		}
		tlsConfig.RootCAs = roots
	}
	dialer := &net.Dialer{Timeout: attemptPhaseTimeout, KeepAlive: idleConnectionTimeout}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   attemptPhaseTimeout,
		ResponseHeaderTimeout: attemptPhaseTimeout,
		MaxIdleConnsPerHost:   maxIdleConnectionsPerHost,
		IdleConnTimeout:       idleConnectionTimeout,
	}
	return &httpClient{client: &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *httpClient) Call(request *http.Request, _ *http.Transport) (*http.Response, error) {
	response, err := c.client.Do(request)
	if err != nil || response.StatusCode < 500 || strings.Contains(response.Header.Get("Content-Type"), "json") {
		return response, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	_ = response.Body.Close()
	return nil, &responseStatusError{statusCode: response.StatusCode}
}
