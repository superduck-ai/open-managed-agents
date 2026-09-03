package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
)

func main() {
	logger := slog.New(logging.NewConsoleHandler(os.Stderr, slog.LevelInfo))
	slog.SetDefault(logger)

	if len(os.Args) != 2 || os.Args[1] != "up" {
		fmt.Fprintf(os.Stderr, "usage: %s up\n", os.Args[0])
		os.Exit(2)
	}

	if err := run(logger); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.Open(ctx, cfg, logger.With("component", "database"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := riverjobs.Migrate(ctx, database, logger.With("component", "river_jobs")); err != nil {
		return fmt.Errorf("migrate River: %w", err)
	}
	logger.Info("database migrations applied")
	return nil
}
