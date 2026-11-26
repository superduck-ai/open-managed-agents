package secrets

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
)

const runtimeProfileAADDomain = "oma.runtime-profile.v1"

type RuntimeProfileBinding struct {
	Scope              string
	OrganizationUUID   string
	RuntimeProfileUUID string
	CredentialRevision int64
}

func (s *Service) SealRuntimeProfile(ctx context.Context, binding RuntimeProfileBinding, plaintext []byte) (Envelope, error) {
	aad, err := runtimeProfileAAD(binding, envelopeFormatVersion)
	if err != nil {
		return Envelope{}, err
	}
	return s.sealWithAAD(ctx, plaintext, aad)
}

func (s *Service) OpenRuntimeProfile(ctx context.Context, binding RuntimeProfileBinding, envelope Envelope) ([]byte, error) {
	aad, err := runtimeProfileAAD(binding, envelope.FormatVersion)
	if err != nil {
		return nil, err
	}
	return s.openWithAAD(ctx, envelope, aad)
}

func runtimeProfileAAD(binding RuntimeProfileBinding, version int) ([]byte, error) {
	if err := validateRuntimeProfileBinding(binding); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	writePrefixString(&buf, runtimeProfileAADDomain)
	writePrefixString(&buf, binding.Scope)
	writePrefixString(&buf, binding.OrganizationUUID)
	writePrefixString(&buf, binding.RuntimeProfileUUID)
	_ = binary.Write(&buf, binary.BigEndian, binding.CredentialRevision)
	_ = binary.Write(&buf, binary.BigEndian, int32(version))
	return buf.Bytes(), nil
}

func validateRuntimeProfileBinding(binding RuntimeProfileBinding) error {
	switch binding.Scope {
	case "platform":
		if binding.OrganizationUUID != "" {
			return fmt.Errorf("%w: platform scope must not include organization_uuid", ErrIncompleteBinding)
		}
	case "organization":
		if binding.OrganizationUUID == "" {
			return fmt.Errorf("%w: organization_uuid", ErrIncompleteBinding)
		}
	default:
		return fmt.Errorf("%w: scope", ErrIncompleteBinding)
	}
	if binding.RuntimeProfileUUID == "" {
		return fmt.Errorf("%w: runtime_profile_uuid", ErrIncompleteBinding)
	}
	if binding.CredentialRevision < 1 {
		return fmt.Errorf("%w: credential_revision", ErrIncompleteBinding)
	}
	return nil
}
