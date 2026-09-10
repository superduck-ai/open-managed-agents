package workerevents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

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
