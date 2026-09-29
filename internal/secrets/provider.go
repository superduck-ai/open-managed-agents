// Package secrets implements provider-independent envelope encryption.
package secrets

import "context"

// WrappedKey is a DEK protected by a KeyProvider's KEK. KeyVersion records
// which KEK wrapped it so an older DEK can be unwrapped during rotation.
type WrappedKey struct {
	Ciphertext []byte
	KeyVersion int64
}

// KeyProvider supplies the KEK used to protect per-secret DEKs. Provider/KMS
// calls must happen outside any DB transaction; Seal/Open never hold one.
type KeyProvider interface {
	Name() string
	// WrapDEK encrypts dek under the current KEK and records its version.
	WrapDEK(ctx context.Context, dek []byte) (WrappedKey, error)
	// UnwrapDEK decrypts a wrapped DEK, selecting the KEK by wrapped.KeyVersion.
	// Unknown versions fail closed.
	UnwrapDEK(ctx context.Context, wrapped WrappedKey) ([]byte, error)
}
