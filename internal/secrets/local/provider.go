// Package local wraps DEKs using locally configured AES-256 KEKs.
package local

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const providerName = "local"

// KeyMaterial is one versioned KEK for the local provider.
type KeyMaterial struct {
	Version int64
	KEK     []byte
}

// Provider protects DEKs with AES-256 KEKs held in memory. Seal uses
// only the current KEK; Open selects among current and decrypt-only versions.
type Provider struct {
	currentVersion int64
	keys           map[int64][]byte
}

// New validates current and decrypt-only KEKs. Key material is
// copied so the caller's slices can be wiped.
func New(current KeyMaterial, decryptOnlyKeys []KeyMaterial) (*Provider, error) {
	currentVersion := current.Version
	if currentVersion == 0 {
		currentVersion = 1
	}
	if currentVersion < 1 {
		return nil, fmt.Errorf("secrets: local KEK version must be >= 1, got %d", current.Version)
	}
	if len(current.KEK) != 32 {
		return nil, fmt.Errorf("secrets: local KEK must be 32 bytes, got %d", len(current.KEK))
	}
	keys := make(map[int64][]byte, 1+len(decryptOnlyKeys))
	keys[currentVersion] = copyKEK(current.KEK)
	for i, entry := range decryptOnlyKeys {
		if entry.Version < 1 {
			return nil, fmt.Errorf("secrets: decrypt_only[%d] version must be >= 1, got %d", i, entry.Version)
		}
		if entry.Version == currentVersion {
			return nil, fmt.Errorf("secrets: decrypt_only[%d] version %d collides with current", i, entry.Version)
		}
		if _, ok := keys[entry.Version]; ok {
			return nil, fmt.Errorf("secrets: decrypt_only[%d] version %d is duplicated", i, entry.Version)
		}
		if len(entry.KEK) != 32 {
			return nil, fmt.Errorf("secrets: decrypt_only[%d] KEK must be 32 bytes, got %d", i, len(entry.KEK))
		}
		keys[entry.Version] = copyKEK(entry.KEK)
	}
	return &Provider{currentVersion: currentVersion, keys: keys}, nil
}

func copyKEK(kek []byte) []byte {
	cp := make([]byte, len(kek))
	copy(cp, kek)
	return cp
}

// Name reports the provider name persisted on envelopes.
func (p *Provider) Name() string { return providerName }

// WrapDEK encrypts dek under the current KEK with a fresh AES-GCM nonce. The
// wrapped form is nonce || ciphertext-with-tag. KeyVersion is the current wrap
// version so envelopes report which KEK sealed them.
func (p *Provider) WrapDEK(_ context.Context, dek []byte) (secrets.WrappedKey, error) {
	kek := p.keys[p.currentVersion]
	gcm, err := newAESGCM(kek)
	if err != nil {
		return secrets.WrappedKey{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return secrets.WrappedKey{}, fmt.Errorf("secrets: read wrap nonce: %w", err)
	}
	// Seal appends ciphertext to dst; seeding dst with nonce yields nonce||ct.
	return secrets.WrappedKey{
		Ciphertext: gcm.Seal(nonce, nonce, dek, nil),
		KeyVersion: p.currentVersion,
	}, nil
}

// UnwrapDEK decrypts a wrapped DEK using the KEK for wrapped.KeyVersion
// (current or decrypt-only). Unknown versions fail closed.
func (p *Provider) UnwrapDEK(_ context.Context, wrapped secrets.WrappedKey) ([]byte, error) {
	kek, ok := p.keys[wrapped.KeyVersion]
	if !ok {
		return nil, fmt.Errorf("secrets: unsupported KEK version %d", wrapped.KeyVersion)
	}
	gcm, err := newAESGCM(kek)
	if err != nil {
		return nil, fmt.Errorf("secrets: build AES-GCM for unwrap: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(wrapped.Ciphertext) < nonceSize {
		return nil, errors.New("secrets: wrapped DEK is too short")
	}
	dek, err := gcm.Open(nil, wrapped.Ciphertext[:nonceSize], wrapped.Ciphertext[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: unwrap DEK: %w", err)
	}
	return dek, nil
}

func newAESGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: build AES cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
