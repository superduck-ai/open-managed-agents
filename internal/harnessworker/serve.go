package harnessworker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func (w *Worker) Serve(ctx context.Context, wait, timeout time.Duration) {
	var activity loopActivity
	w.consume(ctx, wait, func(ctx context.Context, deliveryID int, message jetstream.Msg) {
		worker := *w
		worker.logger = w.logger.With("loop_id", deliveryID)
		loop := deliveryLoop{activity: &activity}
		err := worker.executeDelivery(ctx, message, timeout, &loop)
		loop.finish(ctx, worker.logger, err)
	})
}

func (w *Worker) consume(ctx context.Context, wait time.Duration, handle func(context.Context, int, jetstream.Msg)) {
	var deliveries sync.WaitGroup
	defer deliveries.Wait()
	for deliveryID := 1; ctx.Err() == nil; deliveryID++ {
		message := w.nextDelivery(ctx, wait)
		if message == nil {
			return
		}
		deliveries.Go(func() {
			handle(ctx, deliveryID, message)
		})
	}
}

func (w *Worker) nextDelivery(ctx context.Context, wait time.Duration) jetstream.Msg {
	for ctx.Err() == nil {
		message, err := w.queue.fetchOne(ctx, wait)
		if message != nil {
			return message
		}
		if err == nil || ctx.Err() != nil {
			continue
		}
		w.logger.WarnContext(ctx, "harness worker could not fetch input", "error", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
	return nil
}

type loopActivity struct {
	mu     sync.Mutex
	active int
}

type deliveryLoop struct {
	activity *loopActivity
	started  time.Time
}

func (l *deliveryLoop) logClaim(ctx context.Context, logger *slog.Logger, attrs ...slog.Attr) {
	if l != nil {
		l.activity.mu.Lock()
		defer l.activity.mu.Unlock()
		if l.started.IsZero() {
			l.started = time.Now()
			l.activity.active++
		}
		attrs = append(attrs, slog.Int("active_loops", l.activity.active))
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "harness turn claimed", attrs...)
}

func (l *deliveryLoop) finish(ctx context.Context, logger *slog.Logger, err error) {
	if l.started.IsZero() {
		if err != nil {
			logger.WarnContext(ctx, "harness delivery could not start", "error", err)
		}
		return
	}
	result, level := "finished", slog.LevelInfo
	if err != nil {
		result, level = "failed", slog.LevelWarn
		if errors.Is(err, errNeedsRedelivery) {
			result = "redelivery"
		}
	}
	l.activity.mu.Lock()
	defer l.activity.mu.Unlock()
	l.activity.active--
	attrs := []slog.Attr{
		slog.Int("active_loops", l.activity.active), slog.Int64("elapsed_ms", time.Since(l.started).Milliseconds()),
		slog.String("result", result),
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	logger.LogAttrs(ctx, level, "harness loop finished", attrs...)
}
