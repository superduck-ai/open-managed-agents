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
		fmt.Fprintln(os.Stderr, "oauth-provider-migrate:", err)
		os.Exit(1)
	}
}

func run(args []string, out, stderr io.Writer) error {
	flags := flag.NewFlagSet("oauth-provider-migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apply := flags.Bool("apply", false, "停机后执行加密回填和迁移；默认只读预检")
	fridayPath := flags.String("friday-config", "", "包含 Friday client_id/client_secret 的受控 YAML 文件路径")
	legacyPath := flags.String("legacy-config", "", "包含旧飞书客户端的受控 YAML 文件路径")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	legacy, err := readLegacyClient(*legacyPath)
	if err != nil {
		return err
	}
	friday, err := readFridayClient(*fridayPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return errors.New("configuration_unavailable")
	}
	service, err := secretservice.New(cfg.Vault.MasterKey)
	if err != nil {
		return errors.New("encryption_unavailable")
	}
	logger := slog.New(logging.NewConsoleHandler(stderr, slog.LevelInfo))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	database, err := db.OpenExisting(ctx, cfg, logger)
	if err != nil {
		return errors.New("database_unavailable")
	}
	defer database.Close()
	execute := func() error { return migrate(ctx, database, service, legacy, friday, *apply, out, logger) }
	if *apply {
		if err := database.WithOAuthMaintenanceLock(ctx, execute); err != nil {
			return errors.New("maintenance_failed")
		}
		return nil
	}
	return execute()
}
