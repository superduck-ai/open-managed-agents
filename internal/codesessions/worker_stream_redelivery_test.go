package codesessions

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type stubWorkerSubscription struct {
	messages chan workerevents.Delivery
	errors   chan error
}

func newClosedWorkerSubscription(pending ...workerevents.Delivery) stubWorkerSubscription {
	subscription := stubWorkerSubscription{messages: make(chan workerevents.Delivery, len(pending)), errors: make(chan error)}
	for _, delivery := range pending {
		subscription.messages <- delivery
	}
	close(subscription.messages)
	close(subscription.errors)
	return subscription
}

func (s stubWorkerSubscription) Messages() <-chan workerevents.Delivery { return s.messages }
func (s stubWorkerSubscription) Errors() <-chan error                   { return s.errors }
func (s stubWorkerSubscription) Close() error                           { return nil }

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

func TestNakAbandonedWorkerEventsDecidesByClaimOwnership(t *testing.T) {
	unavailable := errors.New("database unavailable")
	for _, test := range []struct {
		name       string
		validation error
		nak        bool
		failed     bool
	}{
		{"claim released at the same epoch", db.ErrWorkerClaimReleased, true, false},
		{"epoch superseded by a new claim", db.ErrWorkerEpochMismatch, true, false},
		{"claim still held after a plain disconnect", nil, false, false},
		{"code session removed", db.ErrNotFound, false, false},
		{"claim validation unavailable", unavailable, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := &nakRecordingBroker{}
			validate := func(_ context.Context, codeSessionID string, epoch int64) error {
				if codeSessionID != "cse_stale" || epoch != 3 {
					t.Fatalf("validated %s epoch %d", codeSessionID, epoch)
				}
				return test.validation
			}
			err := nakAbandonedWorkerEvents(t.Context(), broker, validate, "cse_stale", 3, trackedWorkerDeliveries("ack-written", "ack-written", "ack-fetched"))
			if (err != nil) != test.failed || (test.failed && !errors.Is(err, unavailable)) {
				t.Fatalf("error = %v", err)
			}
			slices.Sort(broker.naks)
			if test.nak != slices.Equal(broker.naks, []string{"ack-fetched", "ack-written"}) || (!test.nak && len(broker.naks) != 0) {
				t.Fatalf("NAKed %v", broker.naks)
			}
		})
	}
}

func TestNakAbandonedWorkerEventsSkipsEmptyStreamsAndReportsNakFailures(t *testing.T) {
	broker := &nakRecordingBroker{}
	validate := func(context.Context, string, int64) error {
		t.Fatal("validated a claim without deliveries")
		return nil
	}
	empty := &workerStreamDeliveries{}
	empty.track("")
	if err := nakAbandonedWorkerEvents(t.Context(), broker, validate, "cse_stale", 3, empty); err != nil || len(broker.naks) != 0 {
		t.Fatalf("empty stream: err=%v naks=%v", err, broker.naks)
	}
	failing := &nakRecordingBroker{failure: errors.New("nats unavailable")}
	released := func(context.Context, string, int64) error { return db.ErrWorkerClaimReleased }
	if err := nakAbandonedWorkerEvents(t.Context(), failing, released, "cse_stale", 3, trackedWorkerDeliveries("ack-1")); !errors.Is(err, failing.failure) {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkerStreamDrainStopsAtItsOwnDeadline(t *testing.T) {
	open := stubWorkerSubscription{messages: make(chan workerevents.Delivery, 1), errors: make(chan error)}
	open.messages <- workerevents.Delivery{AckSubject: "ack-buffered"}
	deliveries := &workerStreamDeliveries{}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	deliveries.drain(ctx, open)
	if elapsed := time.Since(startedAt); elapsed > workerStreamDrainTimeout {
		t.Fatalf("drain outlived its deadline: %s", elapsed)
	}
	if _, tracked := deliveries.ackSubjects["ack-buffered"]; !tracked {
		t.Fatalf("buffered delivery not tracked: %v", deliveries.ackSubjects)
	}
	closed := &workerStreamDeliveries{}
	closed.drain(t.Context(), newClosedWorkerSubscription(workerevents.Delivery{AckSubject: "ack-fetched"}, workerevents.Delivery{}))
	if len(closed.ackSubjects) != 1 {
		t.Fatalf("closed subscription drained %v", closed.ackSubjects)
	}
}

func TestWorkerStreamAcceptTracksBeforeValidating(t *testing.T) {
	deliveries := &workerStreamDeliveries{}
	rejected := errors.New("epoch mismatch")
	err := deliveries.accept("ack-rejected", func() error {
		if _, tracked := deliveries.ackSubjects["ack-rejected"]; !tracked {
			t.Fatal("delivery validated before it was tracked")
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("error = %v", err)
	}
	if err := deliveries.accept("ack-accepted", func() error { return nil }); err != nil || len(deliveries.ackSubjects) != 2 {
		t.Fatalf("accepted delivery: err=%v tracked=%v", err, deliveries.ackSubjects)
	}
}
