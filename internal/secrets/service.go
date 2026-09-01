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

// TunnelBinding is the authenticated context for one MCP tunnel connector
// token. It deliberately has its own schema instead of overloading the
// vault-specific Binding fields; existing vault ciphertext therefore keeps
// exactly the same AAD bytes and remains decryptable.
type TunnelBinding struct {
	OrganizationUUID string
	WorkspaceUUID    string
	TunnelExternalID string
	TokenExternalID  string
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
	return s.sealWithAAD(ctx, plaintext, aadBytes(binding, envelopeFormatVersion))
}

// Open decrypts an envelope back to plaintext. Any tamper with the ciphertext,
// nonce, wrapped DEK, AAD binding, format version, or provider/version mismatch
// fails closed and returns an error; there is never a plaintext fallback.
func (s *Service) Open(ctx context.Context, binding Binding, envelope Envelope) ([]byte, error) {
	if err := validateBinding(binding); err != nil {
		return nil, err
	}
	return s.openWithAAD(ctx, envelope, aadBytes(binding, envelope.FormatVersion))
}

// SealTunnel encrypts a connector token under a fresh DEK and binds the
// ciphertext to its organization, workspace, tunnel, and token identities.
func (s *Service) SealTunnel(ctx context.Context, binding TunnelBinding, plaintext []byte) (Envelope, error) {
	if err := validateTunnelBinding(binding); err != nil {
		return Envelope{}, err
	}
	return s.sealWithAAD(ctx, plaintext, tunnelAADBytes(binding, envelopeFormatVersion))
}

// OpenTunnel decrypts a connector token and fails closed if any tunnel binding
// field or envelope metadata was changed.
func (s *Service) OpenTunnel(ctx context.Context, binding TunnelBinding, envelope Envelope) ([]byte, error) {
	if err := validateTunnelBinding(binding); err != nil {
		return nil, err
	}
	return s.openWithAAD(ctx, envelope, tunnelAADBytes(binding, envelope.FormatVersion))
}

func (s *Service) sealWithAAD(ctx context.Context, plaintext, aad []byte) (Envelope, error) {
	dek, err := randomBytes(32)
	if err != nil {
		return Envelope{}, fmt.Errorf("secrets: generate DEK: %w", err)
	}
	defer clear(dek)
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
		Ciphertext:    gcm.Seal(nil, nonce, plaintext, aad),
		Nonce:         nonce,
		WrappedDEK:    wrapped.Ciphertext,
		FormatVersion: envelopeFormatVersion,
		KeyProvider:   s.provider.Name(),
		KeyVersion:    wrapped.KeyVersion,
	}, nil
}

func (s *Service) openWithAAD(ctx context.Context, envelope Envelope, aad []byte) ([]byte, error) {
	if envelope.FormatVersion != envelopeFormatVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnknownEnvelopeFormat, envelope.FormatVersion)
	}
	if envelope.KeyProvider != s.provider.Name() {
		return nil, fmt.Errorf("%w: envelope %q, active %q", ErrKeyProviderMismatch, envelope.KeyProvider, s.provider.Name())
	}
	dek, err := s.provider.UnwrapDEK(ctx, WrappedKey{Ciphertext: envelope.WrappedDEK, KeyVersion: envelope.KeyVersion})
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	gcm, err := newAESGCM(dek)
	if err != nil {
		return nil, err
	}
	if len(envelope.Nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("secrets: nonce size mismatch: got %d want %d", len(envelope.Nonce), gcm.NonceSize())
	}
	plaintext, err := gcm.Open(nil, envelope.Nonce, envelope.Ciphertext, aad)
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

func validateTunnelBinding(b TunnelBinding) error {
	switch {
	case b.OrganizationUUID == "":
		return fmt.Errorf("%w: organization_uuid", ErrIncompleteBinding)
	case b.WorkspaceUUID == "":
		return fmt.Errorf("%w: workspace_uuid", ErrIncompleteBinding)
	case b.TunnelExternalID == "":
		return fmt.Errorf("%w: tunnel_external_id", ErrIncompleteBinding)
	case b.TokenExternalID == "":
		return fmt.Errorf("%w: token_external_id", ErrIncompleteBinding)
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

func tunnelAADBytes(b TunnelBinding, formatVersion int) []byte {
	var buf bytes.Buffer
	writePrefixString(&buf, "mcp_tunnel_token")
	writePrefixString(&buf, b.OrganizationUUID)
	writePrefixString(&buf, b.WorkspaceUUID)
	writePrefixString(&buf, b.TunnelExternalID)
	writePrefixString(&buf, b.TokenExternalID)
	_ = binary.Write(&buf, binary.BigEndian, int32(formatVersion))
	return buf.Bytes()
}

func writePrefixString(buf *bytes.Buffer, s string) {
	_ = binary.Write(buf, binary.BigEndian, int32(len(s)))
	buf.WriteString(s)
}
