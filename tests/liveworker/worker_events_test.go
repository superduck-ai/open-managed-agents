package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestLiveWorkerEvents(t *testing.T) {
	e := newLiveEnv(t)
	t.Run("public_HTTP_input_to_JetStream_SSE_and_ACK", func(t *testing.T) {
		f := e.newSession(t)
		s := f.connect(t, "0")
		f.ackInitialize(t, s)
		const text = "live public HTTP input 中文"
		body := map[string]any{"events": []map[string]any{{"type": "user.message", "content": []map[string]string{{"type": "text", "text": text}}}}}
		e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, body, 200)
		event := s.next(t, 10*time.Second)
		if !bytes.Contains(event.Payload, []byte(text)) || event.SessionID != f.code.ExternalID {
			t.Fatal("public event did not reach its worker SSE")
		}
		message, err := e.stream.GetMsg(t.Context(), uint64(event.Sequence))
		requireOK(t, err)
		if !bytes.Contains(message.Data, []byte(text)) {
			t.Fatal("public input missing from JetStream")
		}
		f.ack(t, event.EventID, "processed", 200)
		_, err = e.stream.GetMsg(t.Context(), uint64(event.Sequence))
		if !errors.Is(err, jetstream.ErrMsgNotFound) {
			t.Fatalf("processed public input not removed: %v", err)
		}
	})
	t.Run("unknown_ACK_and_removed_poll", func(t *testing.T) {
		f := e.newSession(t)
		body := e.request(t, "POST", f.path("/worker/events/delivery"), f.token, map[string]any{"worker_epoch": "1", "updates": []map[string]string{{"event_id": "missing", "status": "processed"}}}, 200)
		var result struct {
			Ignored int `json:"ignored"`
		}
		requireOK(t, json.Unmarshal(body, &result))
		if result.Ignored != 1 {
			t.Fatalf("missing ACK ignored=%d", result.Ignored)
		}
		e.request(t, "GET", f.path(""), f.token, nil, 405)
	})
	t.Run("serial_delivery_dedup_progress_and_cursor", func(t *testing.T) {
		f := e.newSession(t)
		s := f.connect(t, "999999999")
		f.ackInitialize(t, s)
		first := payloadFor("first", "hello 中文")
		f.queue(t, first)
		f.queue(t, first)
		f.queue(t, payloadFor("second", "world"))
		got := s.next(t, 10*time.Second)
		if got.EventID != "first" || !bytes.Equal(got.Payload, first) {
			t.Fatal("SSE payload/event ID changed")
		}
		ref, found, err := e.acks.Get(t.Context(), f.code.ExternalID, 1, got.EventID)
		requireOK(t, err)
		if !found || ref.AckSubject == "" {
			t.Fatal("ACK reference not present when SSE arrives")
		}
		s.blocked(t)
		for _, status := range []string{"received", "processing"} {
			iter := e.redis.Scan(t.Context(), 0, "oma:worker-event-ack:v2:"+f.code.ExternalID+":1:*", 100).Iterator()
			if !iter.Next(t.Context()) {
				t.Fatal("test ACK key missing")
			}
			key := iter.Val()
			requireOK(t, e.redis.Expire(t.Context(), key, 10*time.Second).Err())
			f.ack(t, got.EventID, status, 200)
			ttl, err := e.redis.TTL(t.Context(), key).Result()
			requireOK(t, err)
			if ttl < 19*time.Minute || ttl > 20*time.Minute {
				t.Fatalf("ACK TTL was not refreshed: %s", ttl)
			}
			s.blocked(t)
		}
		i := f.consumer(t)
		if i.Config.MaxAckPending != 1 || i.Config.MaxDeliver != -1 || i.Config.AckPolicy != jetstream.AckExplicitPolicy || i.NumAckPending != 1 || i.NumPending != 1 {
			t.Fatalf("unexpected consumer state: ack_pending=%d pending=%d config=%+v", i.NumAckPending, i.NumPending, i.Config)
		}
		if len(i.Config.BackOff) != 3 || i.Config.BackOff[0] != time.Minute || i.Config.BackOff[1] != 5*time.Minute || i.Config.BackOff[2] != 15*time.Minute {
			t.Fatal("unexpected production backoff")
		}
		f.ack(t, got.EventID, "processed", 200)
		next := s.next(t, 10*time.Second)
		if next.EventID != "second" || next.Sequence <= got.Sequence {
			t.Fatal("dedup/order failure")
		}
		_, err = e.stream.GetMsg(t.Context(), uint64(got.Sequence))
		if !errors.Is(err, jetstream.ErrMsgNotFound) {
			t.Fatalf("ACKed message still stored: %v", err)
		}
		f.ack(t, next.EventID, "processed", 200)
		s.cancel()
		if f.consumer(t).Config.Durable == "" {
			t.Fatal("consumer removed after disconnect")
		}
	})
	t.Run("Redis_mapping_loss_and_real_one_minute_redelivery", func(t *testing.T) {
		f := e.newSession(t)
		s := f.connect(t, "0")
		f.ackInitialize(t, s)
		f.queue(t, payloadFor("redis-lost", "redeliver"))
		first := s.next(t, 10*time.Second)
		requireOK(t, e.acks.Delete(t.Context(), f.code.ExternalID, 1, first.EventID))
		body := e.request(t, "POST", f.path("/worker/events/delivery"), f.token, map[string]any{"worker_epoch": "1", "updates": []map[string]string{{"event_id": first.EventID, "status": "processed"}}}, 200)
		var result struct {
			Ignored int `json:"ignored"`
		}
		requireOK(t, json.Unmarshal(body, &result))
		if result.Ignored != 1 {
			t.Fatal("missing Redis mapping was not ignored")
		}
		s.cancel()
		reconnected := f.connect(t, "999999999")
		started := time.Now()
		repeated := reconnected.next(t, 75*time.Second)
		if repeated.EventID != first.EventID || repeated.Sequence != first.Sequence || !bytes.Equal(repeated.Payload, first.Payload) {
			t.Fatal("redelivery changed event identity/payload")
		}
		t.Logf("real production redelivery delay=%s", time.Since(started).Round(time.Millisecond))
		_, found, err := e.acks.Get(t.Context(), f.code.ExternalID, 1, repeated.EventID)
		requireOK(t, err)
		if !found {
			t.Fatal("redelivery did not restore Redis reference")
		}
		f.ack(t, repeated.EventID, "processed", 200)
	})
	t.Run("wrong_session_and_stale_epoch_rejected", func(t *testing.T) {
		f := e.newSession(t)
		other := e.newSession(t)
		s := f.connect(t, "0")
		f.ackInitialize(t, s)
		f.queue(t, payloadFor("epoch-event", "pending"))
		event := s.next(t, 10*time.Second)
		e.request(t, "POST", other.path("/worker/events/delivery"), f.token, map[string]any{"worker_epoch": "1", "updates": []map[string]string{{"event_id": event.EventID, "status": "processed"}}}, 401)
		oldToken := f.token
		recovered, err := e.service.RecoverManagedAgentCodeSession(t.Context(), codesessions.ManagedAgentRecoverInput{Session: f.session, CodeSessionID: f.code.ExternalID})
		requireOK(t, err)
		e.request(t, "POST", f.path("/worker/events/delivery"), oldToken, map[string]any{"worker_epoch": "1", "updates": []map[string]string{{"event_id": event.EventID, "status": "processed"}}}, 401)
		_, err = e.stream.GetMsg(t.Context(), uint64(event.Sequence))
		requireOK(t, err)
		if recovered.WorkerEpoch <= 1 {
			t.Fatal("epoch did not increase")
		}
	})
	t.Run("payload_threshold_roundtrip_and_background_cleanup", func(t *testing.T) {
		f := e.newSession(t)
		s := f.connect(t, "0")
		f.ackInitialize(t, s)
		for _, size := range []int{900*1024 - 513, 900*1024 - 512, 900*1024 - 511, 1024 * 1024} {
			id := strings.Repeat("x", 8) + time.Now().Format("150405.000000000")
			base := payloadFor(id, "")
			payload := payloadFor(id, strings.Repeat("a", size-len(base)))
			if len(payload) != size {
				t.Fatal("incorrect boundary fixture")
			}
			envelope := f.queue(t, payload)
			wantOffload := size+512 > 900*1024
			if (envelope.PayloadRef != nil) != wantOffload {
				t.Fatalf("size=%d offload=%t want=%t", size, envelope.PayloadRef != nil, wantOffload)
			}
			got := s.next(t, 15*time.Second)
			if !bytes.Equal(got.Payload, payload) {
				t.Fatalf("roundtrip differs at size=%d", size)
			}
			if ref := envelope.PayloadRef; ref != nil {
				object, err := e.objects.Open(t.Context(), ref.Key, nil)
				requireOK(t, err)
				requireOK(t, object.Body.Close())
			}
			f.ack(t, got.EventID, "processed", 200)
			if ref := envelope.PayloadRef; ref != nil {
				eventually(t, 45*time.Second, func() bool {
					object, err := e.objects.Open(t.Context(), ref.Key, nil)
					if err == nil {
						_ = object.Body.Close()
						return false
					}
					return errors.Is(err, storage.ErrNotFound)
				})
			}
			t.Logf("payload=%d offloaded=%t roundtrip/ACK/cleanup passed", size, wantOffload)
		}
	})
	t.Run("corrupt_S3_payload_does_not_ACK_or_advance", func(t *testing.T) {
		f := e.newSession(t)
		// Drain initialize before publishing; close SSE so corruption is installed
		// before the API can hydrate the next message.
		s := f.connect(t, "0")
		f.ackInitialize(t, s)
		s.cancel()
		// A fresh consumer subscription is delayed until the object is corrupted.
		// Wait for the closed pull request to exit before adding the large payload.
		time.Sleep(1500 * time.Millisecond)
		envelope := f.queue(t, payloadFor("corrupt", strings.Repeat("a", 950*1024)))
		if envelope.PayloadRef == nil {
			t.Fatal("payload did not offload")
		}
		ref := envelope.PayloadRef
		_, err := e.objects.Upload(t.Context(), ref.Key, strings.NewReader(strings.Repeat("b", int(ref.Size))), storage.UploadOptions{Size: ref.Size, ContentType: "application/json"})
		requireOK(t, err)
		f.queue(t, payloadFor("blocked-after-corrupt", "must not arrive"))
		s = f.connect(t, "0")
		select {
		case got := <-s.events:
			t.Fatalf("corrupt payload was delivered: %s", got.EventID)
		case <-s.errors:
		case <-time.After(10 * time.Second):
			t.Fatal("corrupt payload did not close SSE")
		}
		i := f.consumer(t)
		if i.NumAckPending != 1 || i.NumPending != 1 {
			t.Fatalf("queue advanced after corrupt object: pending=%d ack_pending=%d", i.NumPending, i.NumAckPending)
		}
	})
	t.Run("expired_event_terminates_only_its_session", func(t *testing.T) {
		f := e.newSession(t)
		other := e.newSession(t)
		s := f.connect(t, "0")
		f.ackInitialize(t, s)
		expired := workerevents.EventEnvelope(f.code.ExternalID, "expired-"+f.code.ExternalID, "", "user", "", payloadFor("expired", "expired"), time.Now().Add(-time.Second))
		requireOK(t, e.broker.Publish(t.Context(), expired.EventID, expired))
		eventually(t, 10*time.Second, func() bool {
			code, found, err := e.database.GetCodeSession(context.Background(), f.code.ExternalID)
			return err == nil && found && code.Status == "terminated"
		})
		eventually(t, 10*time.Second, func() bool {
			_, err := e.stream.Consumer(t.Context(), "oma_worker_"+f.code.ExternalID)
			return errors.Is(err, jetstream.ErrConsumerNotFound)
		})
		subject, err := workerevents.Subject(f.code.ExternalID)
		requireOK(t, err)
		_, err = e.stream.GetLastMsgForSubject(t.Context(), subject)
		if !errors.Is(err, jetstream.ErrMsgNotFound) {
			t.Fatalf("expired subject not empty: %v", err)
		}
		code, found, err := e.database.GetCodeSession(t.Context(), other.code.ExternalID)
		requireOK(t, err)
		if !found || code.Status != "active" {
			t.Fatal("unrelated session affected")
		}
		e.request(t, "POST", f.path("/worker/events/delivery"), f.token, map[string]any{"worker_epoch": "1", "updates": []map[string]string{{"event_id": expired.EventID, "status": "processed"}}}, 401)
	})
	t.Run("background_expiry_without_worker_connection", func(t *testing.T) {
		f := e.newSession(t)
		expired := workerevents.EventEnvelope(f.code.ExternalID, "background-expired-"+f.code.ExternalID, "", "user", "", payloadFor("background-expired", "expired"), time.Now().Add(-time.Second))
		requireOK(t, e.broker.Publish(t.Context(), expired.EventID, expired))
		started := time.Now()
		// Do not call RunOnce: verify the existing backend's actual scheduled worker.
		eventually(t, 90*time.Second, func() bool {
			code, found, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
			return err == nil && found && code.Status == "terminated"
		})
		subject, err := workerevents.Subject(f.code.ExternalID)
		requireOK(t, err)
		eventually(t, 5*time.Second, func() bool {
			_, err := e.stream.GetLastMsgForSubject(t.Context(), subject)
			return errors.Is(err, jetstream.ErrMsgNotFound)
		})
		t.Logf("existing backend expiry worker cleaned dedicated session after %s", time.Since(started).Round(time.Millisecond))
	})
}
