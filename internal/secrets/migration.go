package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

var ErrMigrationTarget = errors.New("secrets: target provider failed")

func (s *Service) CheckMigration(ctx context.Context, envelope Envelope) error {
	dek, err := s.unwrapMigrationDEK(ctx, envelope)
	clear(dek)
	return err
}

func (s *Service) MigrateEnvelope(ctx context.Context, envelope Envelope) (Envelope, error) {
	dek, err := s.unwrapMigrationDEK(ctx, envelope)
	defer clear(dek)
	if err != nil {
		return Envelope{}, err
	}
	wrapped, err := s.writeProvider.WrapDEK(ctx, dek)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: wrap DEK: %w", ErrMigrationTarget, err)
	}
	verified, err := s.writeProvider.UnwrapDEK(ctx, wrapped)
	defer clear(verified)
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: verify DEK: %w", ErrMigrationTarget, err)
	}
	if !bytes.Equal(dek, verified) {
		return Envelope{}, fmt.Errorf("%w: DEK verification mismatch", ErrMigrationTarget)
	}
	envelope.WrappedDEK = wrapped.Ciphertext
	envelope.KeyVersion = wrapped.KeyVersion
	envelope.KeyProvider = s.writeProvider.Name()
	return envelope, nil
}

func (s *Service) unwrapMigrationDEK(ctx context.Context, envelope Envelope) ([]byte, error) {
	if envelope.FormatVersion != envelopeFormatVersion {
		return nil, ErrUnknownEnvelopeFormat
	}
	if envelope.KeyVersion <= 0 || len(envelope.Nonce) != 12 || len(envelope.Ciphertext) < 16 || len(envelope.WrappedDEK) == 0 {
		return nil, errors.New("secrets: invalid envelope for migration")
	}
	provider, ok := s.readProviders[envelope.KeyProvider]
	if !ok {
		return nil, ErrKeyProviderUnavailable
	}
	dek, err := provider.UnwrapDEK(ctx, WrappedKey{Ciphertext: envelope.WrappedDEK, KeyVersion: envelope.KeyVersion})
	if err != nil {
		clear(dek)
		return nil, err
	}
	if len(dek) != 32 {
		clear(dek)
		return nil, errors.New("secrets: invalid DEK length")
	}
	return dek, nil
}
