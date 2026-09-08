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

type invalidSubjectScanBroker struct {
	workerevents.Broker
	err error
}

func (b invalidSubjectScanBroker) ScanExpired(context.Context, uint64, int, time.Time) ([]workerevents.ExpiredEvent, uint64, error) {
	return []workerevents.ExpiredEvent{{StreamSequence: 42, InvalidSubject: true}}, 43, b.err
}

func TestExpiryWorkerLogsRemovedInvalidSubjectWithoutAccessingSession(t *testing.T) {
	for _, scanErr := range []error{errors.New("scan failed after deletion"), nil} {
		t.Run(map[bool]string{true: "scan failure", false: "success"}[scanErr != nil], func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			// nil DB 和其余 Broker 方法用于守卫：非法 subject 不得查询或清理 Session。
			worker := NewWorkerEventExpiryWorker(&Service{workerEvents: invalidSubjectScanBroker{err: scanErr}}, logger)
			worker.cursor = 41
			if err := worker.RunOnce(t.Context()); !errors.Is(err, scanErr) {
				t.Fatalf("RunOnce error = %v, want %v", err, scanErr)
			}
			wantCursor := uint64(43)
			if scanErr != nil {
				wantCursor = 41
			}
			if worker.cursor != wantCursor {
				t.Fatalf("cursor = %d, want %d", worker.cursor, wantCursor)
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["level"] != "ERROR" || record["msg"] != "removed worker event with invalid subject" || record["stream_sequence"] != float64(42) {
				t.Fatalf("unexpected deletion alert: %#v", record)
			}
			if len(record) != 4 {
				t.Fatalf("deletion alert must only include time, level, message and sequence: %#v", record)
			}
		})
	}
}
