package sessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

const TitleQueue = "session_title"

type sessionTitleArgs struct {
	db.SessionTitleSource
}

func (sessionTitleArgs) Kind() string { return TitleQueue }

func (h *Handler) RegisterTitleWorker(workers *river.Workers, client *river.Client[*sql.Tx]) {
	h.enqueueTitle = func(ctx context.Context, tx *sql.Tx, source db.SessionTitleSource) error {
		_, err := client.InsertTx(ctx, tx, sessionTitleArgs{source}, &river.InsertOpts{Queue: TitleQueue, MaxAttempts: 3})
		if err != nil {
			h.warnTitleEnqueueFailure(ctx, source.SessionUUID, err)
		}
		return err
	}
	river.AddWorker(workers, &sessionTitleWorker{
		handler: h,
		client: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	})
}

func (h *Handler) warnTitleEnqueueFailure(ctx context.Context, sessionUUID string, err error) {
	attrs := []slog.Attr{
		slog.String("session_uuid", sessionUUID),
		slog.String("error", fmt.Sprintf("%T", err)),
	}
	if postgres, ok := errors.AsType[*pgconn.PgError](err); ok {
		attrs = append(attrs, slog.String("sql_state", postgres.Code))
		if postgres.ConstraintName != "" {
			attrs = append(attrs, slog.String("constraint", postgres.ConstraintName))
		}
	}
	h.logger.LogAttrs(ctx, slog.LevelWarn, "enqueue session title", attrs...)
}

type sessionTitleWorker struct {
	river.WorkerDefaults[sessionTitleArgs]
	handler *Handler
	client  *http.Client
}

func (w *sessionTitleWorker) Work(ctx context.Context, job *river.Job[sessionTitleArgs]) error {
	h := w.handler
	source := job.Args.SessionTitleSource
	session, found, err := h.db.GetSessionByUUID(ctx, source.WorkspaceUUID, source.SessionUUID)
	if err != nil || !found || !session.NeedsTitle() {
		return err
	}
	event, err := h.db.GetSessionEventByUUID(ctx, source.WorkspaceUUID, source.SessionUUID, source.EventUUID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	event, err = h.eventPayloads.RestorePublic(ctx, event)
	if err != nil {
		return err
	}
	prompt, err := sessionTitlePrompt(event)
	if err != nil || prompt == "" {
		return err
	}
	model, err := sessionTitleModel(session.AgentSnapshot)
	if err != nil || model.ID == "" {
		return err
	}
	upstream, err := llmproviders.Resolve(ctx, h.db, h.secretService, session.OrganizationUUID, model.ID)
	if errors.Is(err, llmproviders.ErrNotConfigured) || errors.Is(err, llmproviders.ErrModelNotConfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	title, err := generateSessionTitle(ctx, w.client, upstream, model.InferenceGeo, prompt)
	if err != nil {
		return err
	}
	return h.saveGeneratedSessionTitle(ctx, session, title)
}

func (h *Handler) saveGeneratedSessionTitle(ctx context.Context, session db.Session, title string) error {
	event, err := h.sessionUpdatedEvent(session, map[string]any{"title": title})
	if err != nil {
		return err
	}
	event, saved, err := h.db.SaveGeneratedSessionTitle(ctx, session, title, event)
	if saved {
		h.publishSessionEvents(ctx, []db.SessionEvent{event})
		h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{event})
	}
	return err
}
