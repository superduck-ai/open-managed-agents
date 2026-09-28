package secrets

import "errors"

var (
	// ErrUnknownEnvelopeFormat is returned when an envelope carries an
	// unsupported format version. Fails closed; no plaintext fallback.
	ErrUnknownEnvelopeFormat = errors.New("secrets: unknown envelope format")
	// ErrKeyProviderMismatch is returned when an envelope was sealed by a
	// different provider than the active one.
	ErrKeyProviderMismatch = errors.New("secrets: envelope key provider mismatch")
	// ErrIncompleteBinding is returned when any AAD binding field is empty.
	// Sealing with an incomplete binding would produce ciphertext that cannot
	// be opened once the real identity fields are filled in.
	ErrIncompleteBinding = errors.New("secrets: binding fields must be non-empty")
)
