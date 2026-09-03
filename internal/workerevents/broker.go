package workerevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName    = "OMA_WORKER_INBOUND"
	subjectPrefix = "oma.worker.inbound.v1."
	streamSubject = subjectPrefix + ">"
)

// EnvelopeV1 is the durable, versioned transport copy of a PostgreSQL inbound event.
type EnvelopeV1 struct {
	Version        int             `json:"version"`
	CodeSessionID  string          `json:"code_session_id"`
	EventID        string          `json:"event_id"`
	PayloadEventID string          `json:"payload_event_id,omitempty"`
	SequenceNum    int64           `json:"sequence_num"`
	EventType      string          `json:"event_type"`
	EventSubtype   string          `json:"event_subtype,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

type Delivery struct {
	Envelope EnvelopeV1
	ack      func() error
}

func (d Delivery) Ack() error {
	if d.ack == nil {
		return nil
	}
	return d.ack()
}

type Subscription interface {
	Messages() <-chan Delivery
	Errors() <-chan error
	Close() error
}

type Broker interface {
	Publish(context.Context, EnvelopeV1) error
	Subscribe(context.Context, string) (Subscription, error)
}

func EventEnvelope(codeSessionID, eventID, payloadEventID string, sequenceNum int64, eventType, eventSubtype string, payload json.RawMessage) EnvelopeV1 {
	return EnvelopeV1{Version: 1, CodeSessionID: codeSessionID, EventID: eventID, PayloadEventID: payloadEventID, SequenceNum: sequenceNum, EventType: eventType, EventSubtype: eventSubtype, Payload: payload}
}

func subject(codeSessionID string) (string, error) {
	if codeSessionID == "" || strings.ContainsAny(codeSessionID, ".*> \t\r\n") {
		return "", errors.New("invalid code session ID for worker event subject")
	}
	return subjectPrefix + codeSessionID, nil
}

type JetStreamBroker struct{ js jetstream.JetStream }

func NewJetStream(ctx context.Context, connection *nats.Conn) (*JetStreamBroker, error) {
	if connection == nil || !connection.IsConnected() {
		return nil, nats.ErrDisconnected
	}
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, fmt.Errorf("create worker event JetStream client: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: StreamName, Subjects: []string{streamSubject}, Retention: jetstream.LimitsPolicy,
		Discard: jetstream.DiscardOld, MaxAge: time.Hour, MaxBytes: 1 << 30,
		Storage: jetstream.FileStorage, Replicas: 3, Duplicates: time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure worker event stream: %w", err)
	}
	return &JetStreamBroker{js: js}, nil
}

func (b *JetStreamBroker) Publish(ctx context.Context, envelope EnvelopeV1) error {
	subjectName, err := subject(envelope.CodeSessionID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal worker event envelope: %w", err)
	}
	message := nats.NewMsg(subjectName)
	message.Data = body
	if envelope.EventID != "" {
		message.Header.Set(nats.MsgIdHdr, envelope.EventID)
	}
	if _, err := b.js.PublishMsg(ctx, message); err != nil {
		return fmt.Errorf("publish worker event: %w", err)
	}
	return nil
}

func (b *JetStreamBroker) Subscribe(ctx context.Context, codeSessionID string) (Subscription, error) {
	filter, err := subject(codeSessionID)
	if err != nil {
		return nil, err
	}
	consumer, err := b.js.CreateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		DeliverPolicy: jetstream.DeliverNewPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		FilterSubject: filter, AckWait: time.Minute, InactiveThreshold: time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("create worker event consumer: %w", err)
	}
	subCtx, cancel := context.WithCancel(ctx)
	s := &jetStreamSubscription{js: b.js, consumer: consumer, name: consumer.CachedInfo().Name, ctx: subCtx, cancel: cancel, messages: make(chan Delivery), errors: make(chan error, 1)}
	go s.receive()
	return s, nil
}

type jetStreamSubscription struct {
	js        jetstream.JetStream
	consumer  jetstream.Consumer
	name      string
	ctx       context.Context
	cancel    context.CancelFunc
	messages  chan Delivery
	errors    chan error
	closeOnce sync.Once
}

func (s *jetStreamSubscription) Messages() <-chan Delivery { return s.messages }
func (s *jetStreamSubscription) Errors() <-chan error      { return s.errors }

func (s *jetStreamSubscription) receive() {
	defer close(s.messages)
	defer close(s.errors)
	for s.ctx.Err() == nil {
		batch, err := s.consumer.Fetch(32, jetstream.FetchContext(s.ctx))
		if err != nil {
			if s.ctx.Err() == nil {
				s.report(err)
			}
			return
		}
		for message := range batch.Messages() {
			var envelope EnvelopeV1
			if err := json.Unmarshal(message.Data(), &envelope); err != nil {
				s.report(fmt.Errorf("decode worker event envelope: %w", err))
				return
			}
			delivery := Delivery{Envelope: envelope, ack: message.Ack}
			select {
			case s.messages <- delivery:
			case <-s.ctx.Done():
				return
			}
		}
		if err := batch.Error(); err != nil && !errors.Is(err, context.Canceled) {
			s.report(err)
			return
		}
	}
}

func (s *jetStreamSubscription) report(err error) {
	select {
	case s.errors <- err:
	default:
	}
}

func (s *jetStreamSubscription) Close() error {
	var deleteErr error
	s.closeOnce.Do(func() {
		s.cancel()
		deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		deleteErr = s.js.DeleteConsumer(deleteCtx, StreamName, s.name)
	})
	return deleteErr
}

// MemoryBroker mirrors live-only broker behavior for unit and API tests.
type MemoryBroker struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[uint64]*memorySubscription
}

func NewMemory() *MemoryBroker { return &MemoryBroker{subs: make(map[uint64]*memorySubscription)} }

func (b *MemoryBroker) Publish(ctx context.Context, envelope EnvelopeV1) error {
	b.mu.Lock()
	targets := make([]*memorySubscription, 0, len(b.subs))
	for _, sub := range b.subs {
		if sub.codeSessionID == envelope.CodeSessionID {
			targets = append(targets, sub)
		}
	}
	b.mu.Unlock()
	for _, sub := range targets {
		select {
		case sub.messages <- Delivery{Envelope: envelope}:
		case <-ctx.Done():
			return ctx.Err()
		case <-sub.done:
		}
	}
	return nil
}

func (b *MemoryBroker) Subscribe(ctx context.Context, codeSessionID string) (Subscription, error) {
	if _, err := subject(codeSessionID); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	s := &memorySubscription{broker: b, id: id, codeSessionID: codeSessionID, messages: make(chan Delivery, 256), errors: make(chan error), done: make(chan struct{})}
	b.subs[id] = s
	b.mu.Unlock()
	go func() { <-ctx.Done(); _ = s.Close() }()
	return s, nil
}

type memorySubscription struct {
	broker        *MemoryBroker
	id            uint64
	codeSessionID string
	messages      chan Delivery
	errors        chan error
	done          chan struct{}
	closeOnce     sync.Once
}

func (s *memorySubscription) Messages() <-chan Delivery { return s.messages }
func (s *memorySubscription) Errors() <-chan error      { return s.errors }
func (s *memorySubscription) Close() error {
	s.closeOnce.Do(func() { s.broker.mu.Lock(); delete(s.broker.subs, s.id); close(s.done); s.broker.mu.Unlock() })
	return nil
}
