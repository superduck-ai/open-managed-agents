package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const (
	// StreamName is the shared webhook delivery stream without a namespace.
	StreamName       = "OMA_WEBHOOK_DELIVERY"
	maxMessageBytes  = 64 << 10
	defaultBatchSize = 10
)

type queueNaming struct {
	streamName       string
	deliverySubject  string
	deliveryConsumer string
}

func newQueueNaming(namespace string) queueNaming {
	if namespace == "" {
		return queueNaming{streamName: StreamName, deliverySubject: "oma.webhook.delivery.v1", deliveryConsumer: "oma_webhook_delivery"}
	}
	upper := strings.ToUpper(namespace)
	return queueNaming{
		streamName:       "OMA_" + upper + "_WEBHOOK_DELIVERY",
		deliverySubject:  "oma." + namespace + ".webhook.delivery.v1",
		deliveryConsumer: "oma_" + namespace + "_webhook_delivery",
	}
}

// Envelope is the queue boundary. Event preserves the original signed payload.
// Destination configuration is deliberately fetched only when consuming.
type Envelope struct {
	Version       int             `json:"version"`
	WorkspaceUUID string          `json:"workspace_uuid"`
	EndpointUUID  string          `json:"endpoint_uuid"`
	Event         json.RawMessage `json:"event"`
}

type Publisher interface {
	Publish(context.Context, Envelope) error
}

// Queue shares the process connection; it never drains or closes that connection.
type Queue struct {
	js       jetstream.JetStream
	consumer jetstream.Consumer
	naming   queueNaming
}

func NewQueue(ctx context.Context, conn *nats.Conn, cfg config.WebhookStreamConfig, delivery config.WebhookConfig, namespace string) (*Queue, error) {
	if conn == nil || !conn.IsConnected() {
		return nil, nats.ErrDisconnected
	}
	if err := config.ValidateNATSNamespace(namespace); err != nil {
		return nil, fmt.Errorf("webhook namespace: %w", err)
	}
	naming := newQueueNaming(namespace)
	js, err := jetstream.New(conn)
	if err != nil {
		return nil, err
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: naming.streamName, Subjects: []string{naming.deliverySubject}, Storage: jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy, Discard: jetstream.DiscardNew,
		MaxBytes: cfg.MaxBytes, MaxAge: cfg.MaxAge, Replicas: cfg.Replicas,
		MaxMsgSize: maxMessageBytes, Duplicates: min(2*time.Minute, cfg.MaxAge),
	})
	if err != nil {
		return nil, fmt.Errorf("ensure webhook stream: %w", err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, naming.streamName, jetstream.ConsumerConfig{
		Name: naming.deliveryConsumer, Durable: naming.deliveryConsumer, FilterSubject: naming.deliverySubject,
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		MaxDeliver: webhookMaxAttempts(delivery), AckWait: max(time.Minute, webhookTimeout(delivery)+30*time.Second),
		MaxAckPending: 3000, MaxRequestBatch: defaultBatchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure webhook consumer: %w", err)
	}
	return &Queue{js: js, consumer: consumer, naming: naming}, nil
}

func (q *Queue) Publish(ctx context.Context, envelope Envelope) error {
	event, err := envelope.event()
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return errors.New("encode webhook envelope")
	}
	if len(body) > maxMessageBytes {
		return errors.New("webhook envelope exceeds 64 KiB")
	}
	_, err = q.js.Publish(ctx, q.naming.deliverySubject, body, jetstream.WithMsgID(event.ID+":"+envelope.EndpointUUID), jetstream.WithRetryAttempts(0))
	return err
}

func (e Envelope) event() (Event, error) {
	var event Event
	if e.Version != 1 {
		return event, errors.New("unsupported webhook envelope version")
	}
	if _, err := uuid.Parse(e.WorkspaceUUID); err != nil {
		return event, errors.New("invalid webhook workspace UUID")
	}
	if _, err := uuid.Parse(e.EndpointUUID); err != nil {
		return event, errors.New("invalid webhook endpoint UUID")
	}
	if json.Unmarshal(e.Event, &event) != nil || event.ID == "" || event.Type != "event" || event.Data.Type == "" || event.Data.ID == "" {
		return event, errors.New("invalid webhook event")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.CreatedAt); err != nil {
		return event, errors.New("invalid webhook occurrence time")
	}
	return event, nil
}
