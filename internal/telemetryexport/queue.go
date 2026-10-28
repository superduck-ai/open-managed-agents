package telemetryexport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const (
	StreamName          = "OMA_TELEMETRY_EXPORT"
	deliverySubject     = "oma.telemetry.export.v1"
	deliveryConsumer    = "oma_telemetry_export"
	destinationHeader   = "Oma-Destination-Uuid"
	contentPolicyHeader = "Oma-Content-Policy"
	maxMessageBytes     = 1 << 20
)

type Queue struct {
	js       jetstream.JetStream
	consumer jetstream.Consumer
}

func NewQueue(ctx context.Context, conn *nats.Conn, stream config.WebhookStreamConfig, delivery config.TelemetryExportConfig) (*Queue, error) {
	if conn == nil || !conn.IsConnected() {
		return nil, nats.ErrDisconnected
	}
	js, err := jetstream.New(conn)
	if err != nil {
		return nil, err
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: StreamName, Subjects: []string{deliverySubject}, Storage: jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy, Discard: jetstream.DiscardNew,
		MaxBytes: stream.MaxBytes, MaxAge: stream.MaxAge, Replicas: stream.Replicas,
		MaxMsgSize: maxMessageBytes, Duplicates: min(2*time.Minute, stream.MaxAge),
	})
	if err != nil {
		return nil, fmt.Errorf("ensure telemetry export stream: %w", err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Name: deliveryConsumer, Durable: deliveryConsumer, FilterSubject: deliverySubject,
		DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
		MaxDeliver: delivery.MaxAttempts, AckWait: max(time.Minute, delivery.Timeout*2+30*time.Second),
		MaxAckPending: 3000, MaxRequestBatch: 10,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure telemetry export consumer: %w", err)
	}
	return &Queue{js: js, consumer: consumer}, nil
}

func (q *Queue) Publish(ctx context.Context, delivery Delivery) error {
	if delivery.DestinationUUID == "" || len(delivery.Payload) == 0 || len(delivery.Payload) > maxMessageBytes-4096 {
		return errors.New("invalid telemetry delivery")
	}
	digest := sha256.Sum256(append([]byte(delivery.DestinationUUID+"\x00"+delivery.ContentPolicy+"\x00"), delivery.Payload...))
	message := &nats.Msg{Subject: deliverySubject, Data: delivery.Payload, Header: nats.Header{}}
	message.Header.Set(destinationHeader, delivery.DestinationUUID)
	message.Header.Set(contentPolicyHeader, delivery.ContentPolicy)
	_, err := q.js.PublishMsg(ctx, message, jetstream.WithMsgID(hex.EncodeToString(digest[:])), jetstream.WithRetryAttempts(0))
	return err
}
