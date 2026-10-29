package codesessions

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type nakRecordingBroker struct {
	workerevents.Broker
	naks    []string
	failure error
}

func (b *nakRecordingBroker) Nak(_ context.Context, ackSubject string) error {
	b.naks = append(b.naks, ackSubject)
	return b.failure
}

func trackedWorkerDeliveries(ackSubjects ...string) *workerStreamDeliveries {
	deliveries := &workerStreamDeliveries{}
	for _, ackSubject := range ackSubjects {
		deliveries.track(ackSubject)
	}
	return deliveries
}

func TestNakAbandonedWorkerEventsDecidesByEpochOwnership(t *testing.T) {
	unavailable := errors.New("database unavailable")
	for _, test := range []struct {
		name       string
		validation error
		nak        bool
		failed     bool
	}{
		{"epoch validation unavailable", unavailable, false, true},
		{"code session removed", db.ErrNotFound, false, false},
		{"epoch still current after a plain disconnect", nil, false, false},
		{"epoch superseded by a new claim", db.ErrWorkerEpochMismatch, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := &nakRecordingBroker{}
			validate := func(_ context.Context, codeSessionID string, epoch int64) error {
				if codeSessionID != "cse_stale" || epoch != 3 {
					t.Fatalf("validated %s epoch %d", codeSessionID, epoch)
				}
				return test.validation
			}
			err := nakAbandonedWorkerEvents(t.Context(), broker, validate, "cse_stale", 3, trackedWorkerDeliveries("ack-written", "ack-written", "ack-dropped"))
			if (err != nil) != test.failed || (test.failed && !errors.Is(err, unavailable)) {
				t.Fatalf("error = %v", err)
			}
			slices.Sort(broker.naks)
			if test.nak != slices.Equal(broker.naks, []string{"ack-dropped", "ack-written"}) || (!test.nak && len(broker.naks) != 0) {
				t.Fatalf("NAKed %v", broker.naks)
			}
		})
	}
}

func TestNakAbandonedWorkerEventsSkipsEmptyStreamsAndReportsNakFailures(t *testing.T) {
	broker := &nakRecordingBroker{}
	validate := func(context.Context, string, int64) error {
		t.Fatal("validated an epoch without deliveries")
		return nil
	}
	if err := nakAbandonedWorkerEvents(t.Context(), broker, validate, "cse_stale", 3, trackedWorkerDeliveries("")); err != nil || len(broker.naks) != 0 {
		t.Fatalf("empty stream: err=%v naks=%v", err, broker.naks)
	}
	failing := &nakRecordingBroker{failure: errors.New("nats unavailable")}
	superseded := func(context.Context, string, int64) error { return db.ErrWorkerEpochMismatch }
	if err := nakAbandonedWorkerEvents(t.Context(), failing, superseded, "cse_stale", 3, trackedWorkerDeliveries("ack-1")); !errors.Is(err, failing.failure) {
		t.Fatalf("error = %v", err)
	}
}
