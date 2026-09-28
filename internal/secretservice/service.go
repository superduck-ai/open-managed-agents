// Package secretservice assembles key providers at the application boundary.
// The secrets, vaults and db packages do not depend on provider plugins.
package secretservice

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/secrets/aliyunkms"
	"github.com/superduck-ai/open-managed-agents/internal/secrets/hashicorpvault"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

// Option configures runtime dependencies at the application assembly boundary.
type Option func(*options)

type options struct{ kmsCA string }

// WithKMSCA supplies a PEM trust bundle scoped to this KMS client. It does not
// change process-wide trust or disable certificate verification.
func WithKMSCA(pem string) Option {
	return func(o *options) { o.kmsCA = pem }
}

// New builds exactly one provider without remote probes. Failure never selects another.
func New(mk config.MasterKeyConfig, opts ...Option) (*secrets.Service, error) {
	if err := config.ValidateMasterKey(mk); err != nil {
		return nil, err
	}
	var settings options
	for _, option := range opts {
		option(&settings)
	}
	var provider secrets.KeyProvider
	var err error
	switch mk.EffectiveProvider() {
	case "local":
		provider, err = localProvider(*mk.Local)
	case "hashicorp_vault":
		cfg := mk.HashicorpVault
		provider, err = hashicorpvault.New(hashicorpvault.Config{
			Address: cfg.Address, TransitMount: cfg.TransitMount,
			KeyName: cfg.KeyName, TokenFile: cfg.TokenFile,
		})
	case "aliyun_kms":
		cfg := mk.AliyunKMS
		provider, err = aliyunkms.New(aliyunkms.Config{
			Endpoint: cfg.Endpoint, KeyID: cfg.KeyID,
			AccessKeyID: cfg.AccessKeyID, AccessKeySecret: cfg.AccessKeySecret,
			SecurityToken: cfg.SecurityToken, CA: settings.kmsCA,
		})
	default:
		return nil, errors.New("unsupported vault.master_key.provider")
	}
	if err != nil {
		return nil, err
	}
	return secrets.NewService(provider), nil
}

func localProvider(mk config.LocalKeyConfig) (secrets.KeyProvider, error) {
	kek, err := localkeys.ResolveKEK(mk.Kek, mk.KekFile)
	if err != nil {
		return nil, err
	}
	defer clear(kek)
	current := localkeys.KeyMaterial{Version: mk.EffectiveVersion(), KEK: kek}
	decryptOnly := make([]localkeys.KeyMaterial, 0, len(mk.DecryptOnly))
	defer func() {
		for _, key := range decryptOnly {
			clear(key.KEK)
		}
	}()
	for i, entry := range mk.DecryptOnly {
		resolved, err := localkeys.ResolveKEK(entry.Kek, entry.KekFile)
		if err != nil {
			return nil, fmt.Errorf("vault.master_key.local.decrypt_only[%d]: %w", i, err)
		}
		decryptOnly = append(decryptOnly, localkeys.KeyMaterial{Version: entry.Version, KEK: resolved})
	}
	return localkeys.New(current, decryptOnly)
}
