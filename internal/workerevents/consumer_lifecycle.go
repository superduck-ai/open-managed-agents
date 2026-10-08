package workerevents

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

func (b *JetStreamBroker) refreshConsumerInactivity(ctx context.Context, stream jetstream.Stream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	consumers := stream.ListConsumers(ctx)
	for info := range consumers.Info() {
		cfg := info.Config
		if !workerConsumer(cfg) || cfg.InactiveThreshold == b.consumerInactiveThreshold {
			continue
		}
		cfg.InactiveThreshold = b.consumerInactiveThreshold
		if _, err := stream.UpdateConsumer(ctx, cfg); err != nil && !errors.Is(err, jetstream.ErrConsumerDoesNotExist) && !errors.Is(err, jetstream.ErrConsumerNotFound) {
			return fmt.Errorf("update worker event consumer inactivity: %w", err)
		}
	}
	if err := consumers.Err(); err != nil {
		return fmt.Errorf("list worker event consumers: %w", err)
	}
	return nil
}

func workerConsumer(cfg jetstream.ConsumerConfig) bool {
	sessionID, valid := sessionFromSubject(cfg.FilterSubject)
	if !valid || len(cfg.FilterSubjects) != 0 || cfg.DeliverSubject != "" {
		return false
	}
	for _, lane := range deliveryLanes {
		subject, err := lane.subject(sessionID)
		if err == nil && subject == cfg.FilterSubject && cfg.Name == lane.consumerName(sessionID) && cfg.Durable == cfg.Name {
			return true
		}
	}
	return false
}
