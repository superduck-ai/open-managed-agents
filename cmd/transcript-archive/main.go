// transcript-archive exports or restores private history during an operator maintenance window.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "transcript-archive:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("transcript-archive", flag.ContinueOnError)
	flags.SetOutput(stderr)
	mode := flags.String("mode", "export", "export or restore; pause archive workers first")
	organization := flags.String("organization", "", "organization UUID")
	workspace := flags.String("workspace", "", "workspace UUID")
	session := flags.String("code-session", "", "code session UUID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *mode != "export" && *mode != "restore" {
		return errors.New("mode must be export or restore")
	}
	scope := db.TranscriptScope{OrganizationUUID: strings.TrimSpace(*organization), WorkspaceUUID: strings.TrimSpace(*workspace), CodeSessionUUID: strings.TrimSpace(*session)}
	for _, value := range []string{scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionUUID} {
		if _, err := uuid.Parse(value); err != nil {
			return errors.New("organization, workspace and code-session must be UUIDs")
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(logging.NewConsoleHandler(stderr, slog.LevelInfo)).With("component", "transcript-archive-cli")
	slog.SetDefault(logger)
	database, err := db.Open(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	defer database.Close()
	client, err := storage.New(cfg.Storage)
	if err != nil {
		return err
	}
	objects, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		return err
	}
	service, err := transcriptretention.New(database, objects, cfg.TranscriptArchive, logger)
	if err != nil {
		return err
	}
	if *mode == "restore" {
		return service.Restore(context.Background(), scope)
	}
	return service.Export(context.Background(), scope, stdout)
}
