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

func TestMemorySubscriptionCloseEndsMessages(t *testing.T) {
	broker := NewMemory()
	subscription, err := broker.Subscribe(t.Context(), "cse_memory_close")
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-subscription.Messages():
		if open {
			t.Fatal("closed subscription delivered an event")
		}
	case <-time.After(time.Second):
		t.Fatal("closed subscription kept its message channel open")
	}
}

func TestMemorySupersededSubscriptionCloseEndsMessages(t *testing.T) {
	broker := NewMemory()
	sessionID := "cse_memory_superseded"
	stale, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for open := true; open; {
		select {
		case _, open = <-stale.Messages():
		case <-deadline:
			t.Fatal("superseded subscription kept its message channel open after Close")
		}
	}
	publishControlTestEvent(t, broker, sessionID, "evt_after_supersede", "user", "")
	if delivery := receiveWorkerDelivery(t, replacement); delivery.Envelope.EventID != "evt_after_supersede" {
		t.Fatalf("replacement delivery = %s", delivery.Envelope.EventID)
	}
}
