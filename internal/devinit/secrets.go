package devinit

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	brainClaimTokenBytes = 32
	apiKeyBytes          = 32
)

var fixedSecretFiles = []string{
	"upstream-proxy-ca-key.pem",
	"code-session-jwt-ed25519.pem",
}

// CopyFixedSecrets copies the two stable private keys into the worktree state
// directory. Existing identical content is kept; differing content is a
// conflict and never overwritten.
func CopyFixedSecrets(sourceDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return err
	}
	for _, name := range fixedSecretFiles {
		if err := copyFixedSecret(filepath.Join(sourceDir, name), filepath.Join(targetDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFixedSecret(source, target string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("fixed secret %s: %w", source, err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("fixed secret %s must be a regular file", source)
	}
	targetInfo, err := os.Stat(target)
	sameFile := err == nil && os.SameFile(sourceInfo, targetInfo)
	if err == nil && !sameFile {
		if !targetInfo.Mode().IsRegular() {
			return fmt.Errorf("fixed secret target %s must be a regular file", target)
		}
		equal, err := sameFileContent(source, target)
		if err != nil {
			return err
		}
		if equal {
			return os.Chmod(target, 0o600)
		}
		return fmt.Errorf("fixed secret %s already exists with different content; refusing to overwrite", target)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if sameFile {
		return os.Chmod(target, 0o600)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func sameFileContent(first, second string) (bool, error) {
	firstData, err := os.ReadFile(first)
	if err != nil {
		return false, err
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		return false, err
	}
	return bytes.Equal(firstData, secondData), nil
}

// NewRandomToken returns a URL-safe random token of the given byte length.
func NewRandomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// NewAPIKeyValue mirrors the console key shape while deriving the random
// payload from crypto/rand instead of any user attribute.
func NewAPIKeyValue() (string, error) {
	token, err := NewRandomToken(apiKeyBytes)
	if err != nil {
		return "", err
	}
	return "sk-ant-api03-" + token, nil
}

// WriteBrainClaimToken persists the per-environment claim token.
func WriteBrainClaimToken(secretsDir, token string) (string, error) {
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(secretsDir, "brain-claim-token")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
