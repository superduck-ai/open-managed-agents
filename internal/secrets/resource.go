package secrets

import (
	"bytes"
	"context"
	"encoding/binary"
)

// ResourceBinding allows ciphertext copies within the same organization and workspace.
type ResourceBinding struct {
	OrganizationUUID string
	WorkspaceUUID    string
}

func (s *Service) SealResource(ctx context.Context, binding ResourceBinding, plaintext []byte) (Envelope, error) {
	aad, err := resourceAAD(binding, envelopeFormatVersion)
	if err != nil {
		return Envelope{}, err
	}
	return s.seal(ctx, aad, plaintext)
}

func (s *Service) OpenResource(ctx context.Context, binding ResourceBinding, envelope Envelope) ([]byte, error) {
	aad, err := resourceAAD(binding, envelope.FormatVersion)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, aad, envelope)
}

// resourceAAD encoding is part of the ciphertext compatibility contract; resource IDs are excluded.
func resourceAAD(binding ResourceBinding, version int) ([]byte, error) {
	if binding.OrganizationUUID == "" || binding.WorkspaceUUID == "" {
		return nil, ErrIncompleteBinding
	}
	var buf bytes.Buffer
	for _, value := range []string{"oma.git-resource.workspace", binding.OrganizationUUID, binding.WorkspaceUUID} {
		writePrefixString(&buf, value)
	}
	_ = binary.Write(&buf, binary.BigEndian, int32(version))
	return buf.Bytes(), nil
}
