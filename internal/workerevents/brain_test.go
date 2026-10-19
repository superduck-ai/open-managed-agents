package workerevents

import (
	"encoding/json"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestBrainTasksSurviveSubscriberStartupAndDeduplicate(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	broker, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1})
	if err != nil {
		t.Fatal(err)
	}
	task := BrainTask{SessionID: "cse_task", WorkerEpoch: 7, IngressToken: "fixture", ModelToken: "model"}
	for range 2 {
		if err := broker.PublishBrainTask(t.Context(), task); err != nil {
			t.Fatal(err)
		}
	}
	consumer, err := broker.js.CreateOrUpdateConsumer(t.Context(), "OMA_BRAIN_TASKS", jetstream.ConsumerConfig{Durable: "oma_brain", FilterSubject: "oma.brain.tasks.v1", AckPolicy: jetstream.AckExplicitPolicy})
	if err != nil {
		t.Fatal(err)
	}
	message, err := consumer.Next(jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var received BrainTask
	if err := json.Unmarshal(message.Data(), &received); err != nil {
		t.Fatal(err)
	}
	if received != task || message.Headers().Get("Nats-Msg-Id") != "cse_task:7" {
		t.Fatal("task changed in transit")
	}
	if err := message.DoubleAck(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err := consumer.Info(t.Context())
	if err != nil || info.NumPending != 0 || info.NumAckPending != 0 {
		t.Fatalf("task not drained: %v %v", info, err)
	}
}
