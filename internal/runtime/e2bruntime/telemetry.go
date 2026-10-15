package e2bruntime

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func observeProviderCall(ctx context.Context, operation string, sandboxID string) (context.Context, func(error)) {
	ctx, span := telemetry.Start(ctx, "oma.e2b."+operation,
		telemetry.AttrOperation.String(operation),
		telemetry.AttrProviderSandboxID.String(sandboxID),
	)
	startedAt := time.Now()
	return ctx, func(err error) {
		defer span.End()
		outcome := telemetry.Outcome(err)
		if errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
			outcome = "not_found"
		}
		telemetry.RecordSandboxProviderCall(ctx, operation, outcome, time.Since(startedAt))
		telemetry.SetErrorStatus(span, err)
	}
}
