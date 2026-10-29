package db

import (
	"context"
	"errors"
	"log/slog"

	"github.com/superduck-ai/yourbatis"
)

type statementLogger struct {
	logger *slog.Logger
	next   yourbatis.Logger
}

func newStatementLogger(logger *slog.Logger) statementLogger {
	return statementLogger{logger: logger, next: yourbatis.SlogLogger{Logger: logger}}
}

func (l statementLogger) LogSQL(ctx context.Context, event yourbatis.Event) {
	if event.Err != nil && ctx.Err() != nil && (errors.Is(event.Err, context.Canceled) || errors.Is(event.Err, context.DeadlineExceeded)) {
		l.logger.DebugContext(ctx, "yourbatis statement canceled", "statement", event.Statement.ID, "duration", event.Duration)
		return
	}
	l.next.LogSQL(ctx, event)
}
