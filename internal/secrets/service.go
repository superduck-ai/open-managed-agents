package secrets

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// envelopeFormatVersion is the on-disk format of the envelope and the AAD
// derivation. Bumping it changes the AAD bytes, so existing ciphertext must be
// re-sealed. It is bound into the AAD so an attacker cannot swap versions.
const envelopeFormatVersion = 1

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

// Binding is the per-credential context bound into the authenticated associated
// data (AAD). All four fields are required and non-empty. A ciphertext moved to
// another org/workspace/vault/credential fails to decrypt, so a stolen row is
// useless elsewhere.
type Binding struct {
	OrganizationUUID     string
	WorkspaceUUID        string
	VaultExternalID      string
	CredentialExternalID string
}

// Envelope is the sealed form of a credential secret, mapped 1:1 to the
// vault_credentials envelope columns. All fields are required for Open.
type Envelope struct {
	Ciphertext    []byte
	Nonce         []byte
	WrappedDEK    []byte
	FormatVersion int
	KeyProvider   string
	KeyVersion    int64
}

// Service seals and opens credential secrets using envelope encryption. It
// holds no plaintext or DEK between calls; Seal/Open materialize a DEK only for
// the duration of one operation and wipe it before returning.
type Service struct {
	provider KeyProvider
}

// NewService returns a Service backed by the given provider.
func NewService(provider KeyProvider) *Service {
	return &Service{provider: provider}
}

// Seal encrypts plaintext under a fresh one-time DEK and returns the envelope.
func (s *Service) Seal(ctx context.Context, binding Binding, plaintext []byte) (Envelope, error) {
	if err := validateBinding(binding); err != nil {
		return Envelope{}, err
	}
	dek, err := randomBytes(32) // AES-256
	if err != nil {
		return Envelope{}, fmt.Errorf("secrets: generate DEK: %w", err)
	}
	defer wipe(dek)

	gcm, err := newAESGCM(dek)
	if err != nil {
		return Envelope{}, err
	}
	nonce, err := randomBytes(gcm.NonceSize())
	if err != nil {
		return Envelope{}, fmt.Errorf("secrets: generate nonce: %w", err)
	}
	wrapped, err := s.provider.WrapDEK(ctx, dek)
	if err != nil {
		return Envelope{}, fmt.Errorf("secrets: wrap DEK: %w", err)
	}
	return Envelope{
		Ciphertext:    gcm.Seal(nil, nonce, plaintext, aadBytes(binding, envelopeFormatVersion)),
		Nonce:         nonce,
		WrappedDEK:    wrapped.Ciphertext,
		FormatVersion: envelopeFormatVersion,
		KeyProvider:   s.provider.Name(),
		KeyVersion:    wrapped.KeyVersion,
	}, nil
}

// Open decrypts an envelope back to plaintext. Any tamper with the ciphertext,
// nonce, wrapped DEK, AAD binding, format version, or provider/version mismatch
// fails closed and returns an error; there is never a plaintext fallback.
func (s *Service) Open(ctx context.Context, binding Binding, env Envelope) ([]byte, error) {
	if err := validateBinding(binding); err != nil {
		return nil, err
	}
	if env.FormatVersion != envelopeFormatVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnknownEnvelopeFormat, env.FormatVersion)
	}
	if env.KeyProvider != s.provider.Name() {
		return nil, fmt.Errorf("%w: envelope %q, active %q", ErrKeyProviderMismatch, env.KeyProvider, s.provider.Name())
	}
	dek, err := s.provider.UnwrapDEK(ctx, WrappedKey{Ciphertext: env.WrappedDEK, KeyVersion: env.KeyVersion})
	if err != nil {
		return nil, err
	}
	defer wipe(dek)

	gcm, err := newAESGCM(dek)
	if err != nil {
		return nil, err
	}
	if len(env.Nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("secrets: nonce size mismatch: got %d want %d", len(env.Nonce), gcm.NonceSize())
	}
	plaintext, err := gcm.Open(nil, env.Nonce, env.Ciphertext, aadBytes(binding, env.FormatVersion))
	if err != nil {
		return nil, fmt.Errorf("secrets: open ciphertext: %w", err)
	}
	return plaintext, nil
}

func validateBinding(b Binding) error {
	switch {
	case strings.TrimSpace(b.OrganizationUUID) == "":
		return fmt.Errorf("%w: organization_uuid", ErrIncompleteBinding)
	case strings.TrimSpace(b.WorkspaceUUID) == "":
		return fmt.Errorf("%w: workspace_uuid", ErrIncompleteBinding)
	case strings.TrimSpace(b.VaultExternalID) == "":
		return fmt.Errorf("%w: vault_external_id", ErrIncompleteBinding)
	case strings.TrimSpace(b.CredentialExternalID) == "":
		return fmt.Errorf("%w: credential_external_id", ErrIncompleteBinding)
	default:
		return nil
	}
}

// aadBytes derives the deterministic AAD from the binding and format version.
// UUID and external ID strings are length-prefixed so different field values
// cannot collide. The format version is included so it is integrity-protected;
// the KEK version is intentionally excluded to allow re-wrapping DEKs without
// touching the ciphertext.
func aadBytes(b Binding, formatVersion int) []byte {
	var buf bytes.Buffer
	writePrefixString(&buf, b.OrganizationUUID)
	writePrefixString(&buf, b.WorkspaceUUID)
	writePrefixString(&buf, b.VaultExternalID)
	writePrefixString(&buf, b.CredentialExternalID)
	_ = binary.Write(&buf, binary.BigEndian, int32(formatVersion))
	return buf.Bytes()
}

func writePrefixString(buf *bytes.Buffer, s string) {
	_ = binary.Write(buf, binary.BigEndian, int32(len(s)))
	buf.WriteString(s)
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
