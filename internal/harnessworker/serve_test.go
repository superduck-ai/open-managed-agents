package harnessworker

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestHarnessLoopCountsOnlyClaimedDeliveries(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	var activity loopActivity
	first, second := deliveryLoop{activity: &activity}, deliveryLoop{activity: &activity}
	invalid := deliveryLoop{activity: &activity}
	invalid.finish(t.Context(), logger, errNeedsRedelivery)
	first.logClaim(t.Context(), logger)
	second.logClaim(t.Context(), logger)
	first.logClaim(t.Context(), logger)
	second.finish(t.Context(), logger, errNeedsRedelivery)
	first.finish(t.Context(), logger, nil)

	type record struct {
		Message string `json:"msg"`
		Active  *int   `json:"active_loops"`
		Result  string `json:"result"`
	}
	records := json.NewDecoder(strings.NewReader(output.String()))
	var rejected record
	if err := records.Decode(&rejected); err != nil || rejected.Active != nil || rejected.Message != "harness delivery could not start" {
		t.Fatalf("unclaimed delivery affected the count: %+v, %v", rejected, err)
	}
	for index, want := range []int{1, 2, 2, 1, 0} {
		var got record
		if err := records.Decode(&got); err != nil || got.Active == nil || *got.Active != want {
			t.Fatalf("record %d: %+v, %v, want active_loops=%d", index, got, err, want)
		}
		if index == 3 && got.Result != "redelivery" {
			t.Fatalf("redelivery logged as %+v", got)
		}
	}
}

func TestHarnessServerKeepsOnePullOpen(t *testing.T) {
	queue := harnessTestQueue(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	worker := New(nil, queue, "", slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Serve(ctx, time.Second, time.Second)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	polling := 0
	for polling < 5 {
		select {
		case <-ctx.Done():
			t.Fatal("worker did not poll the consumer")
		case <-ticker.C:
		}
		info, err := queue.consumer.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumWaiting > 1 {
			t.Fatalf("worker opened %d pull requests while idle", info.NumWaiting)
		}
		if info.NumWaiting == 1 {
			polling++
		}
	}
}

func TestHarnessConsumerStartsAllDeliveriesBeforeAnyFinish(t *testing.T) {
	queue := harnessTestQueue(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const deliveryCount = 8
	for range deliveryCount {
		if _, err := queue.js.Publish(ctx, taskSubject, []byte("input")); err != nil {
			t.Fatal(err)
		}
	}

	started := make(chan int, deliveryCount)
	release := make(chan struct{}, deliveryCount)
	results := make(chan error, deliveryCount)
	done := make(chan struct{})
	worker := New(nil, queue, "", slog.New(slog.DiscardHandler))
	go func() {
		defer close(done)
		worker.consume(ctx, time.Second, func(ctx context.Context, deliveryID int, message jetstream.Msg) {
			started <- deliveryID
			select {
			case <-release:
				results <- message.DoubleAck(ctx)
			case <-ctx.Done():
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	seen := make(map[int]bool)
	for range deliveryCount {
		id := waitHarnessDelivery(t, ctx, started)
		if seen[id] {
			t.Fatalf("loop ID %d was reused", id)
		}
		seen[id] = true
	}
	for range deliveryCount {
		release <- struct{}{}
	}
	for range deliveryCount {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("consumer did not settle the deliveries")
		}
	}
}

func waitHarnessDelivery(t *testing.T, ctx context.Context, started <-chan int) int {
	t.Helper()
	select {
	case id := <-started:
		return id
	case <-ctx.Done():
		t.Fatal("consumer did not start a delivery")
		return 0
	}
}
