package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/harnessworker"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/natsclient"
)

func main() {
	logger := slog.New(logging.NewConsoleHandler(os.Stderr, slog.LevelInfo))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("harness worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	apiURL := flag.String("api-url", os.Getenv("OMA_API_URL"), "OMA model gateway base URL")
	wait := flag.Duration("wait", 30*time.Second, "Deadline for one input delivery")
	timeout := flag.Duration("timeout", 2*time.Minute, "Deadline for processing one input delivery")
	flag.Parse()
	baseURL, err := llmproviders.ValidateBaseURL(strings.TrimSpace(*apiURL))
	if err != nil {
		return fmt.Errorf("configure OMA gateway: %w", err)
	}
	if *wait <= 0 || *timeout <= 0 {
		return fmt.Errorf("wait and timeout must be positive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.OpenExisting(ctx, cfg, logger.With("component", "database"))
	if err != nil {
		return err
	}
	defer database.Close()
	connection, err := natsclient.Open(ctx, cfg.NATS, logger.With("component", "nats"))
	if err != nil {
		return err
	}
	defer connection.Close()
	queue, err := harnessworker.Bind(ctx, connection)
	if err != nil {
		return fmt.Errorf("bind harness input consumer: %w", err)
	}
	logger.InfoContext(ctx, "harness worker ready")
	worker := harnessworker.New(database, queue, baseURL, logger.With("component", "harness_worker"))
	claimed, err := worker.RunOnce(ctx, *wait, *timeout)
	if err == nil && !claimed {
		logger.InfoContext(ctx, "no harness input available")
	}
	return err
}
