package codesessions

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
)

const (
	sshSignatureMagic     = "SSHSIG"
	sshSignatureNamespace = "git"
	sshSignatureHash      = "sha512"
	sshEd25519Algorithm   = "ssh-ed25519"
	sshArmorLineLength    = 70
)

// SignGitCommit signs the exact Git payload using the Code Session Ed25519 key
// and returns an armored OpenSSH SSHSIG document.
func (s *Service) SignGitCommit(contents []byte) (string, error) {
	if s == nil || s.credentials == nil {
		return "", errors.New("code-session commit signer is not configured")
	}
	return s.credentials.signGitCommit(contents)
}

// GitSigningPublicKey returns the OpenSSH public-key line that operators add
// to a Git hosting account. The private key is never included.
func (c *SessionCredentials) GitSigningPublicKey() (string, error) {
	if c == nil || len(c.publicKey) != ed25519.PublicKeySize {
		return "", errors.New("code-session signing public key is not configured")
	}
	encoded := base64.StdEncoding.EncodeToString(sshEd25519PublicKeyBlob(c.publicKey))
	return sshEd25519Algorithm + " " + encoded + " oma-code-signing", nil
}

func (c *SessionCredentials) signGitCommit(contents []byte) (string, error) {
	if c == nil || len(c.privateKey) != ed25519.PrivateKeySize || len(c.publicKey) != ed25519.PublicKeySize {
		return "", errors.New("code-session commit signer is not configured")
	}
	digest := sha512.Sum512(contents)
	signedData := append([]byte(nil), sshSignatureMagic...)
	signedData = appendSSHString(signedData, []byte(sshSignatureNamespace))
	signedData = appendSSHString(signedData, nil)
	signedData = appendSSHString(signedData, []byte(sshSignatureHash))
	signedData = appendSSHString(signedData, digest[:])

	signature := ed25519.Sign(c.privateKey, signedData)
	signatureBlob := appendSSHString(nil, []byte(sshEd25519Algorithm))
	signatureBlob = appendSSHString(signatureBlob, signature)

	blob := append([]byte(nil), sshSignatureMagic...)
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, 1)
	blob = append(blob, version...)
	blob = appendSSHString(blob, sshEd25519PublicKeyBlob(c.publicKey))
	blob = appendSSHString(blob, []byte(sshSignatureNamespace))
	blob = appendSSHString(blob, nil)
	blob = appendSSHString(blob, []byte(sshSignatureHash))
	blob = appendSSHString(blob, signatureBlob)
	return armorSSHSignature(blob), nil
}

func sshEd25519PublicKeyBlob(publicKey ed25519.PublicKey) []byte {
	blob := appendSSHString(nil, []byte(sshEd25519Algorithm))
	return appendSSHString(blob, publicKey)
}

func appendSSHString(destination, value []byte) []byte {
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(value)))
	destination = append(destination, length...)
	return append(destination, value...)
}

func armorSSHSignature(blob []byte) string {
	encoded := base64.StdEncoding.EncodeToString(blob)
	var builder strings.Builder
	builder.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for len(encoded) > sshArmorLineLength {
		builder.WriteString(encoded[:sshArmorLineLength])
		builder.WriteByte('\n')
		encoded = encoded[sshArmorLineLength:]
	}
	builder.WriteString(encoded)
	builder.WriteString("\n-----END SSH SIGNATURE-----\n")
	return builder.String()
}
