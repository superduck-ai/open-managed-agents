package workerevents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestJetStreamExpiryScanRetriesFailedInvalidSubjectDeletion(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := broker.js.Publish(t.Context(), subjectPrefix+"csess_bad.extra", []byte(`{`)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deletes := 0
	broker.js, err = jetstream.New(connection, jetstream.WithClientTrace(&jetstream.ClientTrace{
		RequestSent: func(subject string, _ []byte) {
			if strings.Contains(subject, ".STREAM.MSG.DELETE.") {
				deletes++
				if deletes == 2 {
					cancel()
				}
			}
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	events, _, err := broker.ScanExpired(ctx, 0, 2, time.Now())
	if !errors.Is(err, context.Canceled) || len(events) != 1 || !events[0].InvalidSubject || events[0].StreamSequence != 1 {
		t.Fatalf("failed delete must retain earlier deletion alert: events=%+v error=%v", events, err)
	}
	if _, err := stream.GetMsg(t.Context(), 2); err != nil {
		t.Fatalf("failed deletion must retain message: %v", err)
	}
	events, cursor, err := broker.ScanExpired(t.Context(), 0, 2, time.Now())
	if err != nil || cursor != 0 || len(events) != 1 || !events[0].InvalidSubject || events[0].StreamSequence != 2 {
		t.Fatalf("retry: events=%+v cursor=%d error=%v", events, cursor, err)
	}
}

func TestJetStreamExpiryScanRemovesOnlyInvalidSubjects(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	invalidID := "csess_valid.extra"
	matchingJSON, err := json.Marshal(EventEnvelope(invalidID, "invalid", "", "user", "", []byte(`{}`), now.Add(LogicalRetention)))
	if err != nil {
		t.Fatal(err)
	}
	var invalidSequences []uint64
	for _, body := range [][]byte{[]byte(`{`), matchingJSON} {
		ack, err := broker.js.Publish(t.Context(), subjectPrefix+invalidID, body)
		if err != nil {
			t.Fatal(err)
		}
		invalidSequences = append(invalidSequences, ack.Sequence)
	}
	for _, id := range []string{"csess_valid", "csess_bad"} {
		if _, err := broker.js.Publish(t.Context(), subjectPrefix+id, []byte(`{`)); err != nil {
			t.Fatal(err)
		}
	}
	expired := EventEnvelope("csess_expired", "expired", "", "user", "", []byte(`{}`), now.Add(-time.Second))
	if err := broker.Publish(t.Context(), expired.EventID, expired); err != nil {
		t.Fatal(err)
	}
	// 保留同名前缀的正常 Session consumer；非法 subject 不能触发 Session purge。
	if _, err := broker.js.CreateOrUpdateConsumer(t.Context(), StreamName, jetstream.ConsumerConfig{
		Durable: consumerName("csess_valid"), FilterSubject: subjectPrefix + "csess_valid", AckPolicy: jetstream.AckExplicitPolicy,
	}); err != nil {
		t.Fatal(err)
	}
	_, cursor, err := broker.ScanExpired(t.Context(), 0, 2, now)
	if err != nil || cursor != 3 {
		t.Fatalf("first scan: cursor=%d error=%v", cursor, err)
	}
	for _, sequence := range invalidSequences {
		if _, err := stream.GetMsg(t.Context(), sequence); !errors.Is(err, jetstream.ErrMsgNotFound) {
			t.Fatalf("invalid subject at sequence %d was not removed: %v", sequence, err)
		}
	}
	events, cursor, err := broker.ScanExpired(t.Context(), cursor, 3, now)
	if err != nil || cursor != 0 || len(events) != 3 || events[2].Envelope.EventID != expired.EventID {
		t.Fatalf("following batch must remain reachable: events=%+v cursor=%d error=%v", events, cursor, err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 3 || info.State.Consumers != 1 {
		t.Fatalf("valid subjects and consumer must remain: info=%+v error=%v", info, err)
	}
}

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
