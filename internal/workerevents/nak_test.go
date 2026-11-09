package workerevents

import (
	"errors"
	"testing"
	"time"
)

func TestJetStreamNakRedeliversAbandonedEventBeforeBackoff(t *testing.T) {
	broker := lifecycleBroker(t, 5*time.Minute)
	if err := broker.Nak(t.Context(), ""); !errors.Is(err, errAckSubjectEmpty) {
		t.Fatalf("empty NAK subject accepted: %v", err)
	}
	sessionID := "cse_nak"
	stale, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	publishControlTestEvent(t, broker, sessionID, "evt_nak", "user", "")
	abandoned := receiveWorkerDelivery(t, stale)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	assertWorkerSubscriptionClosed(t, stale)
	replacement, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	select {
	case delivery := <-replacement.Messages():
		t.Fatalf("abandoned event redelivered before NAK: %s", delivery.Envelope.EventID)
	case <-time.After(time.Second):
	}
	if err := broker.Nak(t.Context(), abandoned.AckSubject); err != nil {
		t.Fatal(err)
	}
	redelivered := receiveWorkerDelivery(t, replacement)
	if redelivered.Envelope.EventID != "evt_nak" || redelivered.DeliveryCount != 2 {
		t.Fatalf("redelivery = %s attempt %d", redelivered.Envelope.EventID, redelivered.DeliveryCount)
	}
	if err := broker.DoubleAck(t.Context(), redelivered.AckSubject); err != nil {
		t.Fatal(err)
	}
	if err := broker.Nak(t.Context(), redelivered.AckSubject); err != nil {
		t.Fatal(err)
	}
	assertNoWorkerDelivery(t, replacement)
}

func TestMemoryNakRedispatchesOnlyPendingEvents(t *testing.T) {
	broker := NewMemory()
	if err := broker.Nak(t.Context(), ""); !errors.Is(err, errAckSubjectEmpty) {
		t.Fatalf("empty NAK subject accepted: %v", err)
	}
	if err := broker.Nak(t.Context(), "memory-worker-event-ack-404"); err != nil {
		t.Fatalf("unknown NAK subject failed: %v", err)
	}
	sessionID := "cse_memory_nak"
	subscription, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	publishControlTestEvent(t, broker, sessionID, "evt_memory_nak", "user", "")
	first := receiveWorkerDelivery(t, subscription)
	if err := broker.Nak(t.Context(), first.AckSubject); err != nil {
		t.Fatal(err)
	}
	second := receiveWorkerDelivery(t, subscription)
	if second.Envelope.EventID != "evt_memory_nak" {
		t.Fatalf("redelivery = %s", second.Envelope.EventID)
	}
	if err := broker.DoubleAck(t.Context(), second.AckSubject); err != nil {
		t.Fatal(err)
	}
	if err := broker.Nak(t.Context(), second.AckSubject); err != nil {
		t.Fatal(err)
	}
	assertNoWorkerDelivery(t, subscription)
}

func TestMemoryStaleNakDoesNotRedispatchSupersedingDelivery(t *testing.T) {
	broker := NewMemory()
	sessionID := "cse_memory_stale_nak"
	stale, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	publishControlTestEvent(t, broker, sessionID, "evt_superseded", "user", "")
	abandoned := receiveWorkerDelivery(t, stale)
	replacement, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	current := receiveWorkerDelivery(t, replacement)
	if current.AckSubject == abandoned.AckSubject {
		t.Fatalf("redelivery reused ACK subject %s", current.AckSubject)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	if err := broker.Nak(t.Context(), abandoned.AckSubject); err != nil {
		t.Fatal(err)
	}
	assertNoWorkerDelivery(t, replacement)
	if err := broker.DoubleAck(t.Context(), abandoned.AckSubject); err != nil {
		t.Fatalf("ACK of an earlier delivery: %v", err)
	}
	if err := broker.Nak(t.Context(), current.AckSubject); err != nil {
		t.Fatal(err)
	}
	assertNoWorkerDelivery(t, replacement)
}

func TestJetStreamCloseNaksEventFetchedButNotHandedOut(t *testing.T) {
	broker := lifecycleBroker(t, 5*time.Minute)
	sessionID := "cse_close_nak"
	stale, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	publishControlTestEvent(t, broker, sessionID, "evt_fetched", "user", "")
	consumer, err := broker.js.Consumer(t.Context(), StreamName, taskLane.consumerName(sessionID))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := consumer.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.NumAckPending == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reader did not fetch the event")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	assertWorkerSubscriptionClosed(t, stale)
	replacement, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	redelivered := receiveWorkerDelivery(t, replacement)
	if redelivered.Envelope.EventID != "evt_fetched" || redelivered.DeliveryCount != 2 {
		t.Fatalf("redelivery = %s attempt %d", redelivered.Envelope.EventID, redelivered.DeliveryCount)
	}
}
