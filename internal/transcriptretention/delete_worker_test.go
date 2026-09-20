package transcriptretention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptarchive"
)

func TestDeleteWorkerRepairCooldown(t *testing.T) {
	for _, cause := range []error{db.ErrInvalidState, errIntegrity, errStorage, storage.ErrNotFound, storage.ErrAccessDenied, transcriptarchive.ErrIntegrity, transcriptarchive.ErrInvalidSegment} {
		t.Run(cause.Error(), func(t *testing.T) {
			var logs bytes.Buffer
			worker := &deleteWorker{service: &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}}
			job := &river.Job[deleteArgs]{JobRow: &rivertype.JobRow{Metadata: []byte(`{}`)}, Args: deleteArgs{CodeSessionExternalID: "session", WorkspaceUUID: "workspace"}}
			err := worker.handleResult(context.Background(), job, fmt.Errorf("wrapped: %w", cause))
			var snooze *river.JobSnoozeError
			if !errors.As(err, &snooze) || snooze.Duration != 24*time.Hour {
				t.Fatalf("cooldown: %v", err)
			}
			var record struct {
				Level       string `json:"level"`
				Message     string `json:"msg"`
				Error       string `json:"error"`
				SessionID   string `json:"code_session_id"`
				WorkspaceID string `json:"workspace_id"`
			}
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Level != "ERROR" || record.Message != "transcript physical deletion requires repair" || record.Error == "" || record.SessionID != "session" || record.WorkspaceID != "workspace" {
				t.Fatalf("unexpected log: %+v", record)
			}
			logs.Reset()
			job.Metadata = []byte(`{"snoozes":1}`)
			err = worker.handleResult(context.Background(), job, cause)
			if !errors.As(err, &snooze) || logs.Len() != 0 {
				t.Fatalf("repeat refusal: %v, logs %s", err, logs.String())
			}
		})
	}
}

func TestDeleteWorkerTransientAndSuccess(t *testing.T) {
	var logs bytes.Buffer
	worker := &deleteWorker{service: &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}}
	job := &river.Job[deleteArgs]{JobRow: &rivertype.JobRow{Metadata: []byte(`{}`)}, Args: deleteArgs{CodeSessionExternalID: "session", WorkspaceUUID: "workspace"}}
	for _, err := range []error{errors.New("temporary network error"), context.DeadlineExceeded, nil} {
		if got := worker.handleResult(context.Background(), job, err); got != err {
			t.Fatalf("got %v, want %v", got, err)
		}
	}
	if logs.Len() != 0 {
		t.Fatal("transient failures should be reported by River")
	}
}
