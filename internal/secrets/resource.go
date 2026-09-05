package secrets

import (
	"bytes"
	"context"
	"encoding/binary"
)

// ResourceBinding binds a secret to an individual resource of its Session or
// Deployment. It uses a separate AAD domain from vault credentials.
type ResourceBinding struct {
	OrganizationUUID string
	WorkspaceUUID    string
	OwnerKind        string
	OwnerID          string
	ResourceID       string
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

func resourceAAD(binding ResourceBinding, version int) ([]byte, error) {
	if binding.OrganizationUUID == "" || binding.WorkspaceUUID == "" || binding.OwnerID == "" || binding.ResourceID == "" ||
		(binding.OwnerKind != "session" && binding.OwnerKind != "deployment") {
		return nil, ErrIncompleteBinding
	}
	var buf bytes.Buffer
	for _, value := range []string{"oma.github-resource", binding.OrganizationUUID, binding.WorkspaceUUID, binding.OwnerKind, binding.OwnerID, binding.ResourceID} {
		writePrefixString(&buf, value)
	}
	_ = binary.Write(&buf, binary.BigEndian, int32(version))
	return buf.Bytes(), nil
}
