package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "vault-migrate:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("vault-migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	from := flags.String("from", "", "source provider (required)")
	to := flags.String("to", "", "target provider (required)")
	fromVersion := flags.Int64("from-key-version", 0, "source key version; required for same-provider local rotation")
	apply := flags.Bool("apply", false, "save migrated records; omit to preview without database writes")
	flags.Usage = func() {
		fmt.Fprint(stderr, `Migrate all secrets encrypted with the Vault master key between providers.

Usage: go run ./cmd/vault-migrate --from <provider> --to <provider> [--apply]
Providers: local, aliyun_kms, hashicorp_vault

Options:
`)
		flags.PrintDefaults()
		fmt.Fprint(stderr, `
Examples:
  go run ./cmd/vault-migrate --from local --to hashicorp_vault
  go run ./cmd/vault-migrate --from local --to hashicorp_vault --apply

Uses OMA configuration: CONFIG_FILE or config/config.yaml.
Preview verifies source DEKs only; it does not contact the target or write data.
Before --apply, back up the database and stop OMA/workers.
Record failures are counted and left unchanged; any failure returns exit code 1.
Summary and failed record details are written to stdout; logs and final errors to stderr.
`)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments; use --help for usage")
	}
	if !validProvider(*from) || !validProvider(*to) {
		return errors.New("--from and --to are required: choose local, aliyun_kms or hashicorp_vault")
	}
	if *fromVersion < 0 {
		return errors.New("--from-key-version must be positive")
	}
	if *from == *to && (*from != "local" || *fromVersion == 0) {
		return errors.New("same-provider rotation requires local and --from-key-version")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	mk := cfg.Vault.MasterKey
	if (*from == "local" && mk.Local == nil) || (*from == "aliyun_kms" && mk.AliyunKMS == nil) || (*from == "hashicorp_vault" && mk.HashicorpVault == nil) {
		return errors.New("source provider configuration is required")
	}
	if *from == *to && (mk.Local == nil || mk.Local.EffectiveVersion() == *fromVersion) {
		return errors.New("source and current writer key versions must differ")
	}
	mk.Provider = *to
	service, err := secretservice.New(mk)
	if err != nil {
		return err
	}
	logger := slog.New(logging.NewConsoleHandler(stderr, slog.LevelInfo))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	database, err := db.OpenExisting(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer database.Close()
	return migrate(ctx, database, service, *from, *to, *fromVersion, *apply, stdout, logger)
}

func validProvider(name string) bool {
	return name == "local" || name == "aliyun_kms" || name == "hashicorp_vault"
}
