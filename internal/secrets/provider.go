// Package secrets implements envelope encryption for vault credentials.
//
// Vault secrets are sealed per-row with a one-time data-encryption key (DEK);
// each DEK is wrapped by a key-encryption key (KEK) supplied by a KeyProvider.
// The local provider keeps a current wrap KEK plus optional decrypt-only KEKs
// so envelopes sealed under older key_version values remain openable after a
// KEK rotation without rewrapping. Sealing a different provider (Shamir, KMS)
// later does not change the envelope layout, the DB columns, or the public API.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ProviderNameLocal is the KeyProvider.Name reported by LocalKeyProvider and
// persisted on each envelope's key_provider column.
const ProviderNameLocal = "local"

// LocalKeyMaterial is one versioned KEK for the local provider.
type LocalKeyMaterial struct {
	Version int64
	KEK     []byte
}

// WrappedKey is a DEK protected by a KeyProvider's KEK. KeyVersion records
// which KEK wrapped it so an older DEK can be unwrapped during rotation.
type WrappedKey struct {
	Ciphertext []byte
	KeyVersion int64
}

// KeyProvider supplies the KEK used to protect per-secret DEKs. Provider/KMS
// calls must happen outside any DB transaction; Seal/Open never hold one.
type KeyProvider interface {
	// Name is persisted on envelopes and checked at Open to reject envelopes
	// sealed by a different provider mechanism.
	Name() string
	// Prepare initializes the provider at startup (loads/validates the KEK).
	Prepare(ctx context.Context) error
	// WrapDEK encrypts dek under the current KEK and records its version.
	WrapDEK(ctx context.Context, dek []byte) (WrappedKey, error)
	// UnwrapDEK decrypts a wrapped DEK, selecting the KEK by wrapped.KeyVersion.
	// Unknown versions fail closed.
	UnwrapDEK(ctx context.Context, wrapped WrappedKey) ([]byte, error)
}

// LocalKeyProvider protects DEKs with AES-256 KEKs held in memory. Seal uses
// only the current KEK; Open selects among current and decrypt-only versions.
type LocalKeyProvider struct {
	currentVersion int64
	keys           map[int64][]byte
}

// NewLocalService builds a Service from a single 32-byte KEK at version 1.
// Prefer NewLocalServiceWithKeys when configuring rotation.
func NewLocalService(ctx context.Context, kek []byte) (*Service, error) {
	return NewLocalServiceWithKeys(ctx, LocalKeyMaterial{Version: 1, KEK: kek}, nil)
}

// NewLocalServiceWithKeys builds a Service with a current wrap KEK and optional
// decrypt-only KEKs for older envelope key_version values.
func NewLocalServiceWithKeys(ctx context.Context, current LocalKeyMaterial, decryptOnlyKeys []LocalKeyMaterial) (*Service, error) {
	provider, err := NewLocalKeyProvider(current, decryptOnlyKeys)
	if err != nil {
		return nil, err
	}
	if err := provider.Prepare(ctx); err != nil {
		return nil, err
	}
	return NewService(provider), nil
}

// NewLocalKeyProvider validates current and decrypt-only KEKs. Key material is
// copied so the caller's slices can be wiped.
func NewLocalKeyProvider(current LocalKeyMaterial, decryptOnlyKeys []LocalKeyMaterial) (*LocalKeyProvider, error) {
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
	return &LocalKeyProvider{currentVersion: currentVersion, keys: keys}, nil
}

func copyKEK(kek []byte) []byte {
	cp := make([]byte, len(kek))
	copy(cp, kek)
	return cp
}

// Name reports the provider name persisted on envelopes.
func (p *LocalKeyProvider) Name() string { return ProviderNameLocal }

// Prepare is a no-op; KEKs are validated at construction time.
func (p *LocalKeyProvider) Prepare(_ context.Context) error { return nil }

// WrapDEK encrypts dek under the current KEK with a fresh AES-GCM nonce. The
// wrapped form is nonce || ciphertext-with-tag. KeyVersion is the current wrap
// version so envelopes report which KEK sealed them.
func (p *LocalKeyProvider) WrapDEK(_ context.Context, dek []byte) (WrappedKey, error) {
	kek := p.keys[p.currentVersion]
	gcm, err := newAESGCM(kek)
	if err != nil {
		return WrappedKey{}, err
	}
	nonce, err := randomBytes(gcm.NonceSize())
	if err != nil {
		return WrappedKey{}, fmt.Errorf("secrets: read wrap nonce: %w", err)
	}
	// Seal appends ciphertext to dst; seeding dst with nonce yields nonce||ct.
	return WrappedKey{
		Ciphertext: gcm.Seal(nonce, nonce, dek, nil),
		KeyVersion: p.currentVersion,
	}, nil
}

// UnwrapDEK decrypts a wrapped DEK using the KEK for wrapped.KeyVersion
// (current or decrypt-only). Unknown versions fail closed.
func (p *LocalKeyProvider) UnwrapDEK(_ context.Context, wrapped WrappedKey) ([]byte, error) {
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
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: build AES-GCM: %w", err)
	}
	return gcm, nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
