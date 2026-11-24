package harnessworker

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	streamName             = "OMA_HARNESS_TASKS"
	taskSubject            = "oma.harness.task.v1"
	consumerName           = "oma_harness"
	maxDeliveryMessageSize = 512 << 10
)

type Queue struct {
	js       jetstream.JetStream
	consumer jetstream.Consumer
}

func Provision(ctx context.Context, connection *nats.Conn, replicas int) (*Queue, error) {
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, err
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{taskSubject}, Retention: jetstream.WorkQueuePolicy,
		Storage: jetstream.FileStorage, Discard: jetstream.DiscardNew, Replicas: replicas,
		MaxBytes: 64 << 20, MaxMsgSize: maxDeliveryMessageSize, Duplicates: time.Hour,
	})
	if err != nil {
		return nil, err
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable: consumerName, FilterSubject: taskSubject, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 45 * time.Second, MaxRequestBatch: 1, MaxDeliver: -1,
	})
	if err != nil {
		return nil, err
	}
	return &Queue{js: js, consumer: consumer}, nil
}

func Bind(ctx context.Context, connection *nats.Conn) (*Queue, error) {
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, err
	}
	consumer, err := js.Consumer(ctx, streamName, consumerName)
	if err != nil {
		return nil, err
	}
	return &Queue{js: js, consumer: consumer}, nil
}

func (q *Queue) fetchOne(ctx context.Context, wait time.Duration) (jetstream.Msg, error) {
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	batch, err := q.consumer.Fetch(1, jetstream.FetchContext(waitCtx))
	if err != nil {
		return nil, err
	}
	if message, ok := <-batch.Messages(); ok {
		return message, nil
	}
	if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return nil, nil
	}
	return nil, batch.Error()
}
