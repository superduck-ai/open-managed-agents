package sessiontitles

import (
	"context"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/metering"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/titletext"
)

type store interface {
	LeaseSessionTitleJobs(context.Context, string, int) ([]db.SessionTitleJob, error)
	GetSession(context.Context, string, string) (db.Session, bool, error)
	CompleteSessionTitleJob(context.Context, db.SessionTitleJob, string, string) error
}

type generator interface {
	Generate(context.Context, db.Session, string) (string, error)
}

type Worker struct {
	store  store
	model  generator
	logger *slog.Logger
}

func NewWorker(database *db.DB, secretService *secrets.Service, logger *slog.Logger) *Worker {
	logger = logging.LoggerOrDefault(logger)
	client := llmproviders.NewHTTPClient(10 * time.Second)
	client.Transport = &metering.Transport{Base: client.Transport, Store: database,
		Calculator: database.BillingCalculator(), Source: "api", Logger: logger}
	return &Worker{store: database, model: &model{database: database, secrets: secretService, client: client}, logger: logger}
}

func (w *Worker) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "process session title jobs", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (w *Worker) RunOnce(ctx context.Context) error {
	workerID := uuid.NewV4().String()
	jobs, err := w.store.LeaseSessionTitleJobs(ctx, workerID, 4)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Go(func() {
			if err := w.process(ctx, job, workerID); err != nil && ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "complete session title job", "session_id", job.SessionID, "error", err)
			}
		})
	}
	wg.Wait()
	return nil
}

func (w *Worker) process(ctx context.Context, job db.SessionTitleJob, workerID string) error {
	session, found, err := w.store.GetSession(ctx, job.WorkspaceUUID, job.SessionID)
	if err != nil {
		return err
	}
	title := ""
	if found && session.UUID == job.SessionUUID && (session.Title == nil || *session.Title == "") &&
		session.ArchivedAt == nil && session.DeletedAt == nil && session.Status != "terminated" {
		title = titletext.Fallback(job.Input)
		if session.BudgetReachedAt == nil {
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			generated, err := w.model.Generate(callCtx, session, job.Input)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil && generated != "" {
				title = generated
			}
		}
	}
	return w.store.CompleteSessionTitleJob(ctx, job, workerID, title)
}
