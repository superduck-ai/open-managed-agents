// Package secretservice assembles key providers at the application boundary.
// The secrets, vaults and db packages do not depend on provider plugins.
package secretservice

import (
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

func New(mk config.MasterKeyConfig, opts ...Option) (*secrets.Service, error) {
	if err := config.ValidateMasterKey(mk); err != nil {
		return nil, err
	}
	var settings options
	for _, option := range opts {
		option(&settings)
	}
	var writer secrets.KeyProvider
	var providers []secrets.KeyProvider
	for _, name := range []string{"local", "aliyun_kms", "hashicorp_vault"} {
		provider, err := newProvider(name, mk, settings)
		if err != nil {
			return nil, err
		}
		if provider == nil {
			continue
		}
		providers = append(providers, provider)
		if name == mk.EffectiveProvider() {
			writer = provider
		}
	}
	return secrets.NewService(writer, providers...), nil
}

func newProvider(name string, mk config.MasterKeyConfig, settings options) (secrets.KeyProvider, error) {
	switch name {
	case "local":
		if mk.Local != nil {
			return localProvider(*mk.Local)
		}
	case "hashicorp_vault":
		if cfg := mk.HashicorpVault; cfg != nil {
			return hashicorpvault.New(hashicorpvault.Config{
				Address: cfg.Address, TransitMount: cfg.TransitMount,
				KeyName: cfg.KeyName, TokenFile: cfg.TokenFile, Token: cfg.Token, CAFile: cfg.CAFile,
			})
		}
	case "aliyun_kms":
		if cfg := mk.AliyunKMS; cfg != nil {
			return aliyunkms.New(aliyunkms.Config{
				Endpoint: cfg.Endpoint, KeyID: cfg.KeyID,
				AccessKeyID: cfg.AccessKeyID, AccessKeySecret: cfg.AccessKeySecret,
				SecurityToken: cfg.SecurityToken, CA: settings.kmsCA,
			})
		}
	}
	return nil, nil
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
