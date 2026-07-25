package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func main() {
	dir := flag.String("dir", "", "Directory containing .skill archives to import")
	versionsPath := flag.String("versions", "", "Optional JSON object or skill_id=version file mapping skill ids to platform versions")
	prune := flag.Bool("prune", false, "Soft-delete builtin skills not present in --dir")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		log.Fatalf("create object storage client: %v", err)
	}
	store, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		log.Fatalf("bind object storage bucket: %v", err)
	}
	if err := store.Ensure(ctx); err != nil {
		log.Fatalf("ensure object store bucket: %v", err)
	}

	result, err := skills.SeedBuiltinSkills(ctx, database, store, skills.BuiltinSeedOptions{
		Dir:          *dir,
		VersionsPath: *versionsPath,
		Prune:        *prune,
	})
	if err != nil {
		log.Fatalf("seed builtin skills: %v", err)
	}
	fmt.Printf("Imported %d builtin skill(s)", result.Imported)
	if result.Pruned > 0 {
		fmt.Printf(", pruned %d version(s)", result.Pruned)
	}
	fmt.Printf(": %v\n", result.Skills)
}
