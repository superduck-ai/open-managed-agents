package harnessworker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestHarnessQueueDecodesScopedTextInput(t *testing.T) {
	ref := testDeliveryRef()
	input := textInput{GroupID: cloudharness.ThreadID(ref.SessionUUID), Text: "  a text message\n"}
	wrongGroup := textInput{GroupID: cloudharness.ThreadID(uuid.NewString()), Text: input.Text}
	for _, test := range []struct {
		name    string
		input   *textInput
		version int
		kind    string
		want    string
	}{
		{name: "mismatched conversation", version: deliveryVersion, kind: userInputKind, input: &wrongGroup},
		{name: "missing text", version: deliveryVersion, kind: userInputKind},
		{name: "unexpected version", version: 99, input: &input},
		{name: "legacy reference", version: 1, want: "legacy text"},
		{name: "legacy text", version: legacyTextVersion, input: &input, want: input.Text},
		{name: "user text", version: deliveryVersion, kind: userInputKind, input: &input, want: input.Text},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(deliveryEnvelope{Version: test.version, Turn: ref, Kind: test.kind, Input: test.input})
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := decodeDelivery(payload)
			if test.want == "" {
				if err == nil {
					t.Fatal("invalid input accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if envelope.claim().LegacyInput != (test.version == 1) {
				t.Fatal("only legacy reference input should read text from storage")
			}
			step, err := envelope.stepInput(json.RawMessage(`{"content":[{"type":"text","text":"legacy text"}]}`), ref.UUID)
			if err != nil || step.DedupID != ref.UUID || step.Message.GroupID != input.GroupID ||
				step.Message.Content != (cloudharness.UserMessage{Text: test.want}) {
				t.Fatalf("unexpected input: %+v, %v", step, err)
			}
		})
	}
}

func TestHarnessQueueDecodesTaskByReference(t *testing.T) {
	ref := testDeliveryRef()
	envelope := deliveryEnvelope{Version: legacyTaskVersion, Turn: ref}
	invalid, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeDelivery(invalid); err == nil {
		t.Fatal("task without a task UUID was accepted")
	}
	envelope.TaskUUID = uuid.NewString()
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeDelivery(payload)
	if err != nil || got != envelope || got.claim().LegacyInput {
		t.Fatalf("decoded task %+v: %v", got, err)
	}
}

func TestHarnessQueueRejectsInvalidContinuation(t *testing.T) {
	delivery := deliveryEnvelope{
		Version: deliveryVersion, Turn: testDeliveryRef(), Kind: db.HarnessModelTask,
		TaskUUID: uuid.NewString(), ToolUUID: uuid.NewString(),
	}
	if err := delivery.validate(); err == nil {
		t.Fatal("accepted a model continuation without a tool result")
	}
	delivery.Continuation = &modelContinuation{
		Result:    cloudharness.ToolResult{CallID: "call", Text: strings.Repeat("x", sessioncontract.HarnessToolResultMaxBytes+1)},
		ToolCount: 1,
	}
	if err := delivery.validate(); err == nil {
		t.Fatal("accepted an oversized tool result")
	}
	delivery.Continuation.Result.Text = "ok"
	delivery.Continuation.ToolCount = db.HarnessMaxToolCalls + 1
	if err := delivery.validate(); err == nil {
		t.Fatal("accepted a tool count above the turn limit")
	}
}

func harnessTestQueue(t *testing.T) *Queue {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		Host: "127.0.0.1", Port: -1, NoSigs: true, JetStream: true, StoreDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
	go server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("JetStream server did not start")
	}
	connection, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	queue, err := Provision(t.Context(), connection, 1)
	if err != nil {
		t.Fatal(err)
	}
	return queue
}

func TestHarnessJetStreamCarriesInputsAndWakeups(t *testing.T) {
	ctx := t.Context()
	queue := harnessTestQueue(t)
	ref := testDeliveryRef()
	session := db.Session{UUID: ref.SessionUUID, OrganizationUUID: ref.OrganizationUUID, WorkspaceUUID: ref.WorkspaceUUID}
	thread := cloudharness.ThreadID(ref.SessionUUID)
	sender := queue.InputSender(session)
	input := cloudharness.InputMessage{
		GroupID: thread, Content: cloudharness.UserMessage{Text: "read a file"},
		Metadata: json.RawMessage(`{"event":9007199254740993}`),
	}
	if err := sender.SendToInputQueue(ctx, input, ref.UUID); err != nil {
		t.Fatal(err)
	}
	text := takeTestDelivery(t, queue, ref.UUID)
	step, count, err := (&Worker{}).modelInput(ctx, db.HarnessTurnClaim{InputEventUUID: ref.UUID}, text)
	if err != nil || text.Kind != userInputKind || count != 0 || step.DedupID != ref.UUID ||
		step.Message.GroupID != input.GroupID || step.Message.Content != input.Content || string(step.Message.Metadata) != string(input.Metadata) {
		t.Fatalf("user delivery kind = %q, tool count = %d, error = %v", text.Kind, count, err)
	}
	for _, wake := range []struct {
		kind    string
		content cloudharness.Input
	}{
		{choiceResponseKind, cloudharness.ChoiceResponse{}},
		{toolResultKind, cloudharness.ToolResultReady{}},
	} {
		id := uuid.NewString()
		message := cloudharness.InputMessage{GroupID: thread, Content: wake.content}
		for range 2 {
			if err := sender.SendToInputQueue(ctx, message, id); err != nil {
				t.Fatal(err)
			}
		}

		delivery := takeTestDelivery(t, queue, wake.kind+":"+id)
		if delivery.resumeKind() != wake.kind || delivery.Turn.UUID != id || delivery.TaskUUID != "" || delivery.Continuation != nil {
			t.Fatalf("wake delivery = %+v", delivery)
		}
	}
	if message, err := queue.fetchOne(ctx, 100*time.Millisecond); err != nil || message != nil {
		t.Fatalf("unexpected extra delivery: message present %t, error %v", message != nil, err)
	}
}

func TestHarnessSubagentLegacyDeliveryValidation(t *testing.T) {
	ref := testDeliveryRef()
	toolID := uuid.NewString()
	delivery := deliveryEnvelope{Version: deliveryVersion, Turn: ref, Kind: db.HarnessSubagentTask, ToolUUID: toolID, TaskUUID: toolID}
	if err := delivery.validate(); err == nil {
		t.Fatal("accepted a child start with the parent tool task's message ID")
	}
	delivery.TaskUUID = uuid.NewString()
	if err := delivery.validate(); err != nil {
		t.Fatal(err)
	}
}

func takeTestDelivery(t *testing.T, queue *Queue, wantID string) deliveryEnvelope {
	t.Helper()
	message, err := queue.fetchOne(t.Context(), time.Second)
	if err != nil || message == nil {
		t.Fatalf("queue fetch: message present %t, error %v", message != nil, err)
	}
	if id := message.Headers().Get(jetstream.MsgIDHeader); id != wantID {
		t.Fatalf("message ID = %q, want %q", id, wantID)
	}
	delivery, err := decodeDelivery(message.Data())
	if err != nil {
		t.Fatal(err)
	}
	if err := message.DoubleAck(t.Context()); err != nil {
		t.Fatal(err)
	}
	return delivery
}

func testDeliveryRef() db.HarnessTurnRef {
	return db.HarnessTurnRef{UUID: uuid.NewString(), OrganizationUUID: uuid.NewString(), WorkspaceUUID: uuid.NewString(), SessionUUID: uuid.NewString()}
}
