package workerevents

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestJetStreamBrokerTermRejectsEmptyAckSubject(t *testing.T) {
	broker := &JetStreamBroker{}
	if err := broker.Term(t.Context(), ""); !errors.Is(err, errAckSubjectEmpty) {
		t.Fatalf("Term() error = %v, want empty ACK subject", err)
	}
}

func TestJetStreamBrokerTermReportsClosedConnection(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1})
	connection := connectNATS(t, srv.ClientURL())
	connection.Close()
	broker := &JetStreamBroker{connection: connection}
	if err := broker.Term(t.Context(), "worker-event-ack"); !errors.Is(err, nats.ErrConnectionClosed) {
		t.Fatalf("Term() error = %v, want closed connection", err)
	}
}

func TestMemoryBrokerTermRejectsInvalidAckSubject(t *testing.T) {
	broker := NewMemory()
	envelope := EventEnvelope("csess_test", "csev_test", "", "user.message", "", nil, time.Now().Add(time.Hour))
	if err := broker.Publish(t.Context(), envelope.EventID, envelope); err != nil {
		t.Fatal(err)
	}
	subscription, err := broker.Subscribe(t.Context(), envelope.CodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	delivery := receiveWorkerDelivery(t, subscription)
	for _, test := range []struct {
		name       string
		ackSubject string
		wantErr    error
	}{
		{name: "empty", wantErr: errAckSubjectEmpty},
		{name: "unknown", ackSubject: "missing-ack", wantErr: errMemoryAckNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := broker.Term(t.Context(), test.ackSubject); !errors.Is(err, test.wantErr) {
				t.Fatalf("Term() error = %v, want %v", err, test.wantErr)
			}
			if pending := broker.Pending(envelope.CodeSessionID); len(pending) != 1 || pending[0].EventID != envelope.EventID {
				t.Fatalf("pending after rejected Term = %#v, want original event", pending)
			}
		})
	}
	if err := broker.Term(t.Context(), delivery.AckSubject); err != nil {
		t.Fatal(err)
	}
}

func TestJetStreamBrokerTermAdvancesQueueAndEmitsAdvisory(t *testing.T) {
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	advisories, err := connection.SubscribeSync(server.JSAdvisoryConsumerMsgTerminatedPre + "." + StreamName + "." + consumerName("csess_test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = advisories.Unsubscribe() })
	if err := connection.Flush(); err != nil {
		t.Fatal(err)
	}
	terminated := assertTermAdvancesQueue(t, broker)
	message, err := advisories.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("termination advisory: %v", err)
	}
	var advisory server.JSConsumerDeliveryTerminatedAdvisory
	if err := json.Unmarshal(message.Data, &advisory); err != nil {
		t.Fatal(err)
	}
	if advisory.Stream != StreamName || advisory.Consumer != consumerName(terminated.Envelope.CodeSessionID) || advisory.StreamSeq != uint64(terminated.Envelope.SequenceNum) {
		t.Fatalf("termination advisory = %#v, want terminated delivery", advisory)
	}
}

func TestMemoryBrokerTermAdvancesQueue(t *testing.T) {
	broker := NewMemory()
	terminated := assertTermAdvancesQueue(t, broker)
	if pending := broker.Pending(terminated.Envelope.CodeSessionID); len(pending) != 0 {
		t.Fatalf("pending = %#v, want empty queue", pending)
	}
	if pending := broker.Pending("csess_other"); len(pending) != 1 {
		t.Fatalf("other session pending = %#v, want untouched event", pending)
	}
}

func assertTermAdvancesQueue(t *testing.T, broker Broker) Delivery {
	t.Helper()
	const sessionID = "csess_test"
	for _, envelope := range []EnvelopeV1{
		EventEnvelope(sessionID, "csev_first", "", "user.message", "", nil, time.Now().Add(time.Hour)),
		EventEnvelope(sessionID, "csev_second", "", "user.message", "", nil, time.Now().Add(time.Hour)),
		EventEnvelope("csess_other", "csev_other", "", "user.message", "", nil, time.Now().Add(time.Hour)),
	} {
		if err := broker.Publish(t.Context(), envelope.EventID, envelope); err != nil {
			t.Fatal(err)
		}
	}
	subscription, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	first := receiveWorkerDelivery(t, subscription)
	if first.Envelope.EventID != "csev_first" {
		t.Fatalf("first event = %q", first.Envelope.EventID)
	}
	if err := broker.Term(t.Context(), first.AckSubject); err != nil {
		t.Fatal(err)
	}
	second := receiveWorkerDelivery(t, subscription)
	if second.Envelope.EventID != "csev_second" {
		t.Fatalf("event after Term = %q, want second event", second.Envelope.EventID)
	}
	if err := broker.DoubleAck(t.Context(), second.AckSubject); err != nil {
		t.Fatal(err)
	}
	if err := subscription.Close(); err != nil {
		t.Fatal(err)
	}
	reconnected, err := broker.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reconnected.Close() })
	third := EventEnvelope(sessionID, "csev_third", "", "user.message", "", nil, time.Now().Add(time.Hour))
	if err := broker.Publish(t.Context(), third.EventID, third); err != nil {
		t.Fatal(err)
	}
	delivery := receiveWorkerDelivery(t, reconnected)
	if delivery.Envelope.EventID != third.EventID {
		t.Fatalf("event after reconnect = %q, want third event", delivery.Envelope.EventID)
	}
	if err := broker.DoubleAck(t.Context(), delivery.AckSubject); err != nil {
		t.Fatal(err)
	}
	return first
}

func receiveWorkerDelivery(t *testing.T, subscription Subscription) Delivery {
	t.Helper()
	select {
	case delivery, open := <-subscription.Messages():
		if !open {
			t.Fatal("worker event subscription closed")
		}
		return delivery
	case err := <-subscription.Errors():
		t.Fatalf("worker event subscription: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker event")
	}
	return Delivery{}
}
