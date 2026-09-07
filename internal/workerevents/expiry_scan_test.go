package workerevents

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestStoredWorkerEventDoesNotTrustMalformedEnvelope(t *testing.T) {
	storedAt := time.Now().UTC()
	for _, payload := range []string{
		`{`,
		`{"version":2,"code_session_id":"csess_other","expires_at":"2000-01-01T00:00:00Z"}`,
		`{"version":2,"code_session_id":"csess_test"}`,
		`{"version":2,"code_session_id":"csess_test","expires_at":"sensitive-invalid-timestamp"}`,
	} {
		event := storedWorkerEvent(&jetstream.RawStreamMsg{
			Subject: subjectPrefix + "csess_test", Sequence: 7, Time: storedAt, Data: []byte(payload),
		})
		if event.DecodeError != errInvalidStoredEnvelope || event.Envelope.CodeSessionID != "csess_test" ||
			!event.Envelope.ExpiresAt.Equal(storedAt.Add(LogicalRetention)) || event.Envelope.PayloadRef != nil {
			t.Fatalf("unsafe malformed envelope fallback: %#v", event)
		}
	}
}

func TestJetStreamExpiryScanSkipsHolesAndIsolatesMalformedMessages(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(t.Context(), subjectPrefix+"csess_bad", []byte(`{`)); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		ack, err := js.Publish(t.Context(), subjectPrefix+"csess_hole", []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.DeleteMsg(t.Context(), ack.Sequence); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	expired := EventEnvelope("csess_expired", "event_expired", "", "user", "", []byte(`{}`), now.Add(-time.Second))
	if err := broker.Publish(t.Context(), expired.EventID, expired); err != nil {
		t.Fatal(err)
	}
	events, cursor, err := broker.ScanExpired(t.Context(), 0, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || cursor != 0 {
		t.Fatalf("scan = %d events, cursor %d; want two stored messages and end of scan", len(events), cursor)
	}
	if events[0].DecodeError == nil || events[0].Envelope.CodeSessionID != "csess_bad" ||
		events[1].DecodeError != nil || events[1].Envelope.EventID != expired.EventID || events[1].StreamSequence != 7 {
		t.Fatalf("scan results = %#v", events)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 2 {
		t.Fatalf("scan must not ACK or delete messages: info=%+v error=%v", info, err)
	}
	subscription, err := broker.Subscribe(t.Context(), expired.CodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	_ = receiveWorkerDelivery(t, subscription)
	for range 2 {
		if err := broker.PurgeSession(t.Context(), expired.CodeSessionID); err != nil {
			t.Fatalf("idempotent purge: %v", err)
		}
	}
	info, err = stream.Info(t.Context())
	if err != nil || info.State.Msgs != 1 || info.State.Consumers != 0 {
		t.Fatalf("purge must remove only the expired session and its consumer: info=%+v error=%v", info, err)
	}
}
