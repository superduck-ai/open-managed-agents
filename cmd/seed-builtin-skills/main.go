package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(observability.NewConsoleHandler(os.Stderr, slog.LevelInfo)))

	dir := flag.String("dir", "", "Directory containing .skill archives to import")
	versionsPath := flag.String("versions", "", "Optional JSON object or skill_id=version file mapping skill ids to platform versions")
	prune := flag.Bool("prune", false, "Soft-delete builtin skills not present in --dir")
	flag.Parse()

	if err := run(*dir, *versionsPath, *prune); err != nil {
		slog.Error("seed builtin skills failed", "error", err)
		os.Exit(1)
	}
}

func run(dir string, versionsPath string, prune bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		return fmt.Errorf("create object storage client: %w", err)
	}
	store, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		return fmt.Errorf("bind object storage bucket: %w", err)
	}
	if err := store.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure object store bucket: %w", err)
	}

	result, err := skills.SeedBuiltinSkills(ctx, database, store, skills.BuiltinSeedOptions{
		Dir:          dir,
		VersionsPath: versionsPath,
		Prune:        prune,
	})
	if err != nil {
		return fmt.Errorf("seed builtin skills: %w", err)
	}
	fmt.Printf("Imported %d builtin skill(s)", result.Imported)
	if result.Pruned > 0 {
		fmt.Printf(", pruned %d version(s)", result.Pruned)
	}
	fmt.Printf(": %v\n", result.Skills)
	return nil
}
