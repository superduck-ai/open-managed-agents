package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsInvalidInput(t *testing.T) {
	if err := run(nil, ioDiscardForTest{}); err == nil {
		t.Fatal("run() accepted missing key path")
	}
	if err := run([]string{filepath.Join(t.TempDir(), "missing.pem")}, ioDiscardForTest{}); err == nil {
		t.Fatal("run() accepted missing key file")
	}
}

func TestRunPrintsOnlyOpenSSHEd25519PublicKey(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encodedPrivateKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "signing-key.pem")
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedPrivateKey})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	var output bytes.Buffer
	if err := run([]string{keyFile}, &output); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	line := strings.TrimSuffix(output.String(), "\n")
	parts := strings.Fields(line)
	if len(parts) != 3 || parts[0] != "ssh-ed25519" || parts[2] != "oma-code-signing" {
		t.Fatalf("unexpected output: %q", output.String())
	}
	blob, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if !bytes.HasSuffix(blob, publicKey) {
		t.Fatal("printed key does not contain the generated public key")
	}
	if bytes.Contains(output.Bytes(), encodedPrivateKey) || strings.Contains(output.String(), "PRIVATE KEY") {
		t.Fatal("output contains private key material")
	}
}

type ioDiscardForTest struct{}

func (ioDiscardForTest) Write(value []byte) (int, error) {
	return len(value), nil
}
