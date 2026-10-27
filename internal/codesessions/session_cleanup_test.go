package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type retirementPurgeBroker struct {
	workerevents.Broker
	purge func(context.Context, string) error
}

func (b retirementPurgeBroker) PurgeSession(ctx context.Context, id string) error {
	return b.purge(ctx, id)
}

func TestPurgeWorkerEventsContinuesAfterFailure(t *testing.T) {
	var output bytes.Buffer
	var calls []string
	purgeErr := errors.New("NATS unavailable")
	service := &Service{
		logger: slog.New(slog.NewJSONHandler(&output, nil)),
		workerEvents: retirementPurgeBroker{purge: func(ctx context.Context, id string) error {
			calls = append(calls, id)
			if id == "old-worker" {
				return purgeErr
			}
			return nil
		}},
	}
	service.PurgeWorkerEvents(t.Context(), []string{"old-worker", "current-worker"})
	if len(calls) != 2 || calls[0] != "old-worker" || calls[1] != "current-worker" {
		t.Fatalf("purge calls = %v", calls)
	}
	var record struct {
		Level         string `json:"level"`
		Message       string `json:"msg"`
		CodeSessionID string `json:"code_session_id"`
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Level != "WARN" || record.Message != "purge retired worker events" || record.CodeSessionID != "old-worker" || record.Error != purgeErr.Error() {
		t.Fatalf("unexpected warning: %+v", record)
	}
}

func TestPurgeWorkerEventsStopsAfterDeadline(t *testing.T) {
	calls := 0
	service := &Service{
		logger: slog.New(slog.DiscardHandler),
		workerEvents: retirementPurgeBroker{purge: func(ctx context.Context, id string) error {
			calls++
			<-ctx.Done()
			return ctx.Err()
		}},
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	service.purgeWorkerEvents(ctx, []string{"old-worker", "current-worker"})
	if calls != 1 {
		t.Fatalf("purge calls = %d, want 1", calls)
	}
}

func TestPurgeWorkerEventsSurvivesRequestCancellation(t *testing.T) {
	calls := 0
	service := &Service{
		logger: slog.New(slog.DiscardHandler),
		workerEvents: retirementPurgeBroker{purge: func(ctx context.Context, id string) error {
			calls++
			if err := ctx.Err(); err != nil {
				t.Fatalf("cleanup inherited request cancellation: %v", err)
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second {
				t.Fatal("cleanup needs a bounded deadline")
			}
			return nil
		}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service.PurgeWorkerEvents(ctx, []string{"worker"})
	if calls != 1 {
		t.Fatalf("purge calls = %d, want 1", calls)
	}
}
