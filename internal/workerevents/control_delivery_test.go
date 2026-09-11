package workerevents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestMemoryPurgeRejectsInvalidSessionWithoutRemovingReply(t *testing.T) {
	broker := NewMemory()
	const sessionID = "cse_purge_identity"
	sub, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	publishControlTestEvent(t, broker, sessionID, "reply", "control_response", "success")
	reply := receiveWorkerDelivery(t, sub)
	if err := broker.PurgeSession(t.Context(), sessionID+".reply"); err == nil {
		t.Fatal("invalid session ID must not alias another session's reply lane")
	}
	if len(broker.Pending(sessionID)) != 1 {
		t.Fatal("invalid purge removed the valid session's reply")
	}
	if err := broker.DoubleAck(t.Context(), reply.AckSubject); err != nil {
		t.Fatalf("invalid purge removed the valid session's ACK mapping: %v", err)
	}
}

func TestReplyDecodeFailureClosesBothReadersWithoutAcknowledging(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	baselineSubscriptions := connection.NumSubscriptions()
	const sessionID = "cse_reply_decode_failure"
	sub, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	subject, _ := replyLane.subject(sessionID)
	ack, err := broker.js.Publish(t.Context(), subject, []byte(`{`))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-sub.Errors():
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("expected reply decode error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing reply decode error")
	}
	assertWorkerSubscriptionClosed(t, sub)
	assertNATSSubscriptionsReleased(t, connection, baselineSubscriptions)
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.GetMsg(t.Context(), ack.Sequence); err != nil {
		t.Fatalf("decode failure must retain the unacknowledged reply: %v", err)
	}
}

func TestWorkerSubscriptionCloseReleasesIdleAndBlockedReaders(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	for _, queued := range []bool{false, true} {
		name := "idle"
		if queued {
			name = "blocked"
		}
		t.Run(name, func(t *testing.T) {
			baselineSubscriptions := connection.NumSubscriptions()
			sessionID := "cse_close_" + name
			sub, err := broker.Subscribe(t.Context(), sessionID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sub.Close() })
			if queued {
				publishControlTestEvent(t, broker, sessionID, "task", "user", "")
				publishControlTestEvent(t, broker, sessionID, "reply", "control_response", "success")
				// Leave the merged channel unread so both readers block on handoff.
				deadline := time.Now().Add(5 * time.Second)
				for _, lane := range deliveryLanes {
					consumer, err := broker.js.Consumer(t.Context(), StreamName, lane.consumerName(sessionID))
					if err != nil {
						t.Fatal(err)
					}
					for {
						info, err := consumer.Info(t.Context())
						if err != nil {
							t.Fatal(err)
						}
						if info.NumAckPending == 1 {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("reader did not pull queued message")
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
			}
			if err := sub.Close(); err != nil {
				t.Fatal(err)
			}
			assertWorkerSubscriptionClosed(t, sub)
			assertNATSSubscriptionsReleased(t, connection, baselineSubscriptions)
			if queued {
				stream, err := broker.js.Stream(t.Context(), StreamName)
				if err != nil {
					t.Fatal(err)
				}
				info, err := stream.Info(t.Context())
				if err != nil || info.State.Msgs != 2 {
					t.Fatalf("Close must preserve both unacknowledged lanes: %+v %v", info, err)
				}
			}
		})
	}
}

func assertWorkerSubscriptionClosed(t *testing.T, sub Subscription) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	messages, errs := sub.Messages(), sub.Errors()
	for messages != nil || errs != nil {
		select {
		case _, open := <-messages:
			if !open {
				messages = nil
			}
		case _, open := <-errs:
			if !open {
				errs = nil
			}
		case <-deadline:
			t.Fatal("subscription readers did not terminate")
		}
	}
}

func assertNATSSubscriptionsReleased(t *testing.T, connection *nats.Conn, want int) {
	t.Helper()
	// Fetch closes its output before its deferred NATS Unsubscribe returns.
	// Check eventual release, not the ordering of those two deferred operations.
	deadline := time.Now().Add(5 * time.Second)
	for connection.NumSubscriptions() != want {
		if time.Now().After(deadline) {
			t.Fatalf("NATS subscriptions leaked: got %d, want %d", connection.NumSubscriptions(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReplyPurgeRetainsMessagesWhenConsumerDeletionFails(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "cse_purge_failure"
	sub, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	publishControlTestEvent(t, broker, sessionID, "task", "user", "")
	publishControlTestEvent(t, broker, sessionID, "reply", "control_response", "success")
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	broker.js, err = jetstream.New(connection, jetstream.WithClientTrace(&jetstream.ClientTrace{RequestSent: func(subject string, _ []byte) {
		if strings.Contains(subject, ".CONSUMER.DELETE.") && strings.HasSuffix(subject, "_reply") {
			cancel()
		}
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.PurgeSession(ctx, sessionID); err == nil {
		t.Fatal("expected interrupted consumer deletion")
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 2 {
		t.Fatalf("failed consumer deletion must not purge either lane: %+v %v", info, err)
	}
	broker.js, err = jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.PurgeSession(t.Context(), sessionID); err != nil {
		t.Fatal(err)
	}
	info, err = stream.Info(t.Context())
	if err != nil || info.State.Msgs != 0 || info.State.Consumers != 0 {
		t.Fatalf("purge retry did not complete: %+v %v", info, err)
	}
}

func TestSessionPurgeRetriesAfterSecondLaneFailure(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "cse_partial_purge"
	now := time.Now().UTC()
	expired := EventEnvelope(sessionID, "expired-task", "expired-task", "user", "", json.RawMessage(`{}`), now.Add(-time.Second))
	if err := broker.Publish(t.Context(), "expired-task", expired); err != nil {
		t.Fatal(err)
	}
	publishControlTestEvent(t, broker, sessionID, "reply", "control_response", "success")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	purges := 0
	broker.js, err = jetstream.New(connection, jetstream.WithClientTrace(&jetstream.ClientTrace{RequestSent: func(subject string, _ []byte) {
		if strings.Contains(subject, ".STREAM.PURGE.") {
			purges++
			if purges == 2 {
				cancel()
			}
		}
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.PurgeSession(ctx, sessionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected interrupted second purge, got %v", err)
	}
	broker.js, err = jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("failed reply purge must retain the reply: %+v %v", info, err)
	}
	// The expired task is already gone; the reply remains covered by its own
	// retention deadline even if the caller does not retry the failed purge.
	events, _, err := broker.ScanExpired(t.Context(), 0, 10, now.Add(2*time.Hour))
	if err != nil || len(events) != 1 || events[0].Envelope.EventID != "reply" {
		t.Fatalf("remaining reply must stay visible to expiry: %+v %v", events, err)
	}
	if err := broker.PurgeSession(t.Context(), sessionID); err != nil {
		t.Fatal(err)
	}
	info, err = stream.Info(t.Context())
	if err != nil || info.State.Msgs != 0 {
		t.Fatalf("purge retry did not remove remaining reply: %+v %v", info, err)
	}
}

func TestControlDeliveryPreservesTaskOrdering(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	jsBroker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	for name, broker := range map[string]Broker{"memory": NewMemory(), "jetstream": jsBroker} {
		t.Run(name, func(t *testing.T) {
			const sessionID = "cse_control_order"
			sub, err := broker.Subscribe(t.Context(), sessionID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sub.Close() })
			publishControlTestEvent(t, broker, sessionID, "first", "user", "")
			first := receiveWorkerDelivery(t, sub)
			publishControlTestEvent(t, broker, sessionID, "queued", "user", "")
			if err := broker.InProgress(t.Context(), first.AckSubject); err != nil {
				t.Fatal(err)
			}
			assertNoWorkerDelivery(t, sub)
			publishControlTestEvent(t, broker, sessionID, "reply", "control_response", "success")
			reply := receiveWorkerDelivery(t, sub)
			if reply.Envelope.EventID != "reply" {
				t.Fatalf("got %s before approval", reply.Envelope.EventID)
			}
			publishControlTestEvent(t, broker, sessionID, "reply2", "control_response", "error")
			assertNoWorkerDelivery(t, sub)
			if err := broker.DoubleAck(t.Context(), reply.AckSubject); err != nil {
				t.Fatal(err)
			}
			reply2 := receiveWorkerDelivery(t, sub)
			if reply2.Envelope.EventID != "reply2" {
				t.Fatal("task ordering changed")
			}
			if err := broker.DoubleAck(t.Context(), reply2.AckSubject); err != nil {
				t.Fatal(err)
			}
			assertNoWorkerDelivery(t, sub)
			if err := broker.DoubleAck(t.Context(), first.AckSubject); err != nil {
				t.Fatal(err)
			}
			queued := receiveWorkerDelivery(t, sub)
			if queued.Envelope.EventID != "queued" || queued.Envelope.SequenceNum >= reply.Envelope.SequenceNum {
				t.Fatal("queued input must arrive after the newer control response")
			}
			if err := broker.DoubleAck(t.Context(), queued.AckSubject); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkerDeliveryLaneClassification(t *testing.T) {
	for _, tc := range []struct {
		kind, subtype string
		want          deliveryLane
	}{
		{"control_request", "initialize", taskLane},
		{"control_request", "unknown", taskLane},
		{"user", "", taskLane},
		{"future_event", "", taskLane},
		{"control_response", "success", replyLane},
		{"control_response", "error", replyLane},
		{"control_cancel_request", "", taskLane},
		{"control_request", "interrupt", replyLane},
		{"user.interrupt", "", taskLane},
		{"user.tool_result", "", taskLane},
		{"user.custom_tool_result", "", taskLane},
	} {
		if got := laneForEvent(EnvelopeV1{EventType: tc.kind, EventSubtype: tc.subtype}); got != tc.want {
			t.Errorf("%s/%s lane=%q want %q", tc.kind, tc.subtype, got, tc.want)
		}
	}
}

func TestReplySubjectExpiryAndSessionPurge(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "cse_expire_reply"
	sub, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	envelope := EventEnvelope(sessionID, "expired-reply", "reply", "control_response", "success", json.RawMessage(`{}`), time.Now().Add(-time.Second))
	if err := broker.Publish(t.Context(), "expired-reply", envelope); err != nil {
		t.Fatal(err)
	}
	publishControlTestEvent(t, broker, sessionID, "task", "user", "")
	publishControlTestEvent(t, broker, "cse_other", "other", "user", "")
	events, _, err := broker.ScanExpired(t.Context(), 0, 10, time.Now())
	if err != nil || len(events) != 1 || events[0].InvalidSubject || events[0].Envelope.CodeSessionID != sessionID {
		t.Fatalf("reply expiry = %+v, %v", events, err)
	}
	if err := broker.PurgeSession(t.Context(), sessionID); err != nil {
		t.Fatal(err)
	}
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 1 || info.State.Consumers != 0 {
		t.Fatalf("both lanes must be purged without touching another session: %+v, %v", info, err)
	}
}

func TestSessionFromSubjectRejectsUnknownSuffix(t *testing.T) {
	for _, subject := range []string{subjectPrefix + "cse_a.extra", subjectPrefix + "cse_a.reply.reply", "other.cse_a.reply"} {
		if _, valid := sessionFromSubject(subject); valid {
			t.Errorf("accepted %s", subject)
		}
	}
	for _, lane := range deliveryLanes {
		subject, _ := lane.subject("cse_a")
		if id, valid := sessionFromSubject(subject); !valid || id != "cse_a" {
			t.Errorf("rejected %s", subject)
		}
	}
}

func publishControlTestEvent(t *testing.T, broker Broker, sessionID, id, kind, subtype string) {
	t.Helper()
	envelope := EventEnvelope(sessionID, id, id, kind, subtype, json.RawMessage(`{}`), time.Now().Add(time.Hour))
	if err := broker.Publish(t.Context(), id, envelope); err != nil {
		t.Fatal(err)
	}
}

func assertNoWorkerDelivery(t *testing.T, sub Subscription) {
	t.Helper()
	select {
	case delivery := <-sub.Messages():
		t.Fatalf("unexpected delivery %s", delivery.Envelope.EventID)
	case err := <-sub.Errors():
		t.Fatalf("subscription error: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
}
