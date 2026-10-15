package riverjobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/attribute"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

const traceParentMetadataKey = "oma_trace_parent"

type telemetryMiddleware struct {
	river.MiddlewareDefaults
}

func (m *telemetryMiddleware) InsertMany(ctx context.Context, manyParams []*rivertype.JobInsertParams, doInner func(context.Context) ([]*rivertype.JobInsertResult, error)) ([]*rivertype.JobInsertResult, error) {
	traceParent := telemetry.TraceParent(ctx)
	if traceParent != "" {
		for _, params := range manyParams {
			params.Metadata = withTraceParent(params.Metadata, traceParent)
		}
	}
	return doInner(ctx)
}

func (m *telemetryMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	startedAt := time.Now()
	ctx, span := telemetry.StartRoot(ctx, "oma.river."+job.Kind, traceParentFromMetadata(job.Metadata),
		attribute.Int64("oma.river.job.id", job.ID),
		attribute.String("oma.river.job.kind", job.Kind),
		attribute.String("oma.river.queue", job.Queue),
		attribute.Int("oma.river.attempt", job.Attempt),
	)
	err := doInner(ctx)
	outcome := riverOutcome(job, err)
	telemetry.RecordRiverJob(ctx, job.Kind, job.Queue, outcome, time.Since(startedAt), riverQueueWait(job))
	span.SetAttributes(telemetry.AttrOutcome.String(outcome))
	telemetry.SetErrorStatus(span, riverSpanError(outcome, err))
	span.End()
	return err
}

func riverOutcome(job *rivertype.JobRow, err error) string {
	var snooze *rivertype.JobSnoozeError
	var cancel *rivertype.JobCancelError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &snooze):
		return "snoozed"
	case errors.As(err, &cancel):
		return "cancelled"
	case job.Attempt >= job.MaxAttempts:
		return "discarded"
	default:
		return "retry"
	}
}

func riverSpanError(outcome string, err error) error {
	if outcome == "snoozed" {
		return nil
	}
	return err
}

func riverQueueWait(job *rivertype.JobRow) time.Duration {
	if job.AttemptedAt == nil || job.AttemptedAt.Before(job.ScheduledAt) {
		return 0
	}
	return job.AttemptedAt.Sub(job.ScheduledAt)
}

func withTraceParent(metadata []byte, traceParent string) []byte {
	fields := map[string]json.RawMessage{}
	if len(metadata) > 0 && (json.Unmarshal(metadata, &fields) != nil || fields == nil) {
		return metadata
	}
	encoded, err := json.Marshal(traceParent)
	if err != nil {
		return metadata
	}
	fields[traceParentMetadataKey] = encoded
	merged, err := json.Marshal(fields)
	if err != nil {
		return metadata
	}
	return merged
}

func traceParentFromMetadata(metadata []byte) string {
	var fields struct {
		TraceParent string `json:"oma_trace_parent"`
	}
	if len(metadata) == 0 || json.Unmarshal(metadata, &fields) != nil {
		return ""
	}
	return fields.TraceParent
}
