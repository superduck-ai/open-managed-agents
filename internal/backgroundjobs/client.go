// Package backgroundjobs owns River infrastructure shared by resource workers.
package backgroundjobs

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

func Migrate(ctx context.Context, database *db.DB, logger *slog.Logger) error {
	migrator, err := rivermigrate.New(riverdatabasesql.New(database.SQLDB()), &rivermigrate.Config{Schema: "public", Logger: logging.LoggerOrDefault(logger)})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func NewClient(database *db.DB, logger *slog.Logger, workers *river.Workers, queues map[string]river.QueueConfig) (*river.Client[*sql.Tx], error) {
	return river.NewClient(
		riverdatabasesql.NewWithPgxListener(database.SQLDB(), database.ListenerPool()),
		&river.Config{
			Schema:              "public",
			Workers:             workers,
			Queues:              queues,
			Logger:              logging.LoggerOrDefault(logger),
			JobTimeout:          2 * time.Minute,
			SoftStopTimeout:     10 * time.Second,
			DurablePeriodicJobs: river.DurablePeriodicJobsConfig{Enabled: true},
		})
}
