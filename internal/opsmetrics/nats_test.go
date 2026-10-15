package opsmetrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type testJetStream struct {
	jetstream.JetStream
	streams   map[string]jetstream.Stream
	consumers map[string]jetstream.Consumer
}

func (j testJetStream) Stream(_ context.Context, name string) (jetstream.Stream, error) {
	value, ok := j.streams[name]
	if !ok {
		return nil, errors.New("stream unavailable")
	}
	return value, nil
}
func (j testJetStream) Consumer(_ context.Context, _, name string) (jetstream.Consumer, error) {
	value, ok := j.consumers[name]
	if !ok {
		return nil, errors.New("consumer unavailable")
	}
	return value, nil
}

type testStream struct {
	jetstream.Stream
	info *jetstream.StreamInfo
	err  error
}

func (s testStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	return s.info, s.err
}

type testConsumer struct {
	jetstream.Consumer
	info *jetstream.ConsumerInfo
	err  error
}

func (c testConsumer) Info(context.Context) (*jetstream.ConsumerInfo, error) { return c.info, c.err }

func TestReporterCollectsNATSSnapshotsWithoutService(t *testing.T) {
	reporter, _ := reporterForTest(t)
	now := time.Now()
	reporter.deps.Streams = []string{"missing", "broken", "empty", "future", "ready"}
	reporter.deps.Consumers = []ConsumerTarget{{Stream: "stream", Consumer: "missing"}, {Stream: "stream", Consumer: "broken"}, {Stream: "stream", Consumer: "ready"}}
	reporter.js = testJetStream{streams: map[string]jetstream.Stream{
		"broken": testStream{err: errors.New("info unavailable")},
		"empty":  testStream{info: &jetstream.StreamInfo{}},
		"future": testStream{info: &jetstream.StreamInfo{State: jetstream.StreamState{Msgs: 1, FirstTime: now.Add(time.Minute)}}},
		"ready":  testStream{info: &jetstream.StreamInfo{State: jetstream.StreamState{Msgs: 7, FirstTime: now.Add(-10 * time.Second)}}},
	}, consumers: map[string]jetstream.Consumer{
		"broken": testConsumer{err: errors.New("info unavailable")},
		"ready":  testConsumer{info: &jetstream.ConsumerInfo{NumPending: 3, NumAckPending: 2, NumRedelivered: 1}},
	}}
	streams := reporter.collectStreams(t.Context(), now)
	if len(streams) != 3 || streams[0].messages != 0 || streams[0].oldestAge != 0 || streams[1].oldestAge != 0 || streams[2].messages != 7 || streams[2].oldestAge != 10 {
		t.Fatalf("stream snapshots: %v", streams)
	}
	consumers := reporter.collectConsumers(t.Context())
	if len(consumers) != 1 || consumers[0].name != "ready" || consumers[0].pending != 3 || consumers[0].ackPending != 2 || consumers[0].redelivered != 1 {
		t.Fatalf("consumer snapshots: %v", consumers)
	}
}
