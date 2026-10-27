package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	if len(arguments) != 1 {
		return errors.New("usage: code-session-signing-public-key <PKCS#8 Ed25519 private-key-file>")
	}
	credentials, err := codesessions.NewSessionCredentials(config.Config{
		CodeSession: config.CodeSessionConfig{JWTSigningPrivateKeyFile: arguments[0]},
	})
	if err != nil {
		return err
	}
	publicKey, err := credentials.GitSigningPublicKey()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, publicKey)
	return err
}
