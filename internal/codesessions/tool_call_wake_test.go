package codesessions

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestToolCallNotificationFailureLogsStructuredIdentity(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS did not start")
	}
	connection, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	var output bytes.Buffer
	handler := &Handler{toolCallNotifications: connection, logger: slog.New(slog.NewJSONHandler(&output, nil))}
	handler.notifyToolCall(db.SandboxToolCall{UUID: "call-uuid", CodeSessionUUID: "code-uuid", State: "succeeded"})
	var record struct {
		Level    string `json:"level"`
		Message  string `json:"msg"`
		CallUUID string `json:"call_uuid"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Level != "WARN" || record.Message != "tool call notification failed" || record.CallUUID != "call-uuid" || record.Error != nats.ErrConnectionClosed.Error() {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestToolCallWakeReceivesPublishImmediatelyAfterSubscribe(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS did not start")
	}
	listener, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(listener.Close)
	publisher, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	handler := &Handler{toolCallNotifications: listener}
	for range 50 {
		next, closeWait := handler.toolCallWake(t.Context(), []string{"oma.cs.wake.calls"}, 2*time.Second)
		if err := publisher.Publish("oma.cs.wake.calls", nil); err != nil {
			t.Fatal(err)
		}
		if err := publisher.Flush(); err != nil {
			t.Fatal(err)
		}
		woke := next()
		closeWait()
		if !woke {
			t.Fatal("wake published right after subscribing was lost")
		}
	}
}

func TestToolCallNotificationsTargetOnlyInterestedWaiters(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS did not start")
	}
	connection, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	messages := make(chan string, 8)
	subscription, err := connection.Subscribe("oma.>", func(message *nats.Msg) { messages <- message.Subject })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Unsubscribe() })
	if err := connection.Flush(); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{toolCallNotifications: connection, logger: slog.New(slog.DiscardHandler)}
	for _, state := range []string{"dispatched", "running", "cancel_requested", "finalizing", "pending", "succeeded"} {
		handler.notifyToolCall(db.SandboxToolCall{UUID: "call", CodeSessionUUID: "code", State: state})
	}
	if err := connection.Flush(); err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for len(subjects) < 2 {
		select {
		case subject := <-messages:
			subjects = append(subjects, subject)
		case <-time.After(time.Second):
			t.Fatalf("subjects=%v", subjects)
		}
	}
	select {
	case subject := <-messages:
		t.Fatalf("unexpected notification %s", subject)
	case <-time.After(100 * time.Millisecond):
	}
	if subjects[0] != "oma.cs.code.calls" || subjects[1] != "oma.tc.call.done" {
		t.Fatalf("subjects=%v", subjects)
	}
}
