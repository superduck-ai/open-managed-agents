package tunnels

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestNATSBrokerInitializeNotificationKeepsProxySessionIdentity(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("streamed-initialize")
	command.JSONRPC = json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	waiter, err := b.subscribeResponse(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	claimed := pollTestCommands(t, b, "a", channels, 1)[0]
	notification := testTerminalResponse(command.RequestID)
	notification.ResponseType = ResponseTypeJSONRPCNotify
	notification.JSONResponse = json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/message","params":{}}`)
	notification.ResponseHeaders = http.Header{"Mcp-Session-Id": {"private-id"}}
	if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, claimed.ShardToken, notification); err != nil {
		t.Fatal(err)
	}
	record, _, err := b.readRequest(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	premature := testQueuedCommand("before-ready")
	premature.Headers.Set("Mcp-Session-Id", record.SessionID)
	if err := b.Enqueue(t.Context(), "tunnel", premature); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session usable before successful initialize: %v", err)
	}
	terminal := testTerminalResponse(command.RequestID)
	terminal.ResponseHeaders = notification.ResponseHeaders
	if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, claimed.ShardToken, terminal); err != nil {
		t.Fatal(err)
	}
	seen := 0
	result, err := waiter.Wait(t.Context(), func(response TunnelResponse) {
		seen++
		if response.ResponseHeaders.Get("Mcp-Session-Id") != record.SessionID {
			t.Error("notification exposed native session identity before terminal")
		}
	})
	if err != nil || seen != 1 || result.ResponseHeaders.Get("Mcp-Session-Id") != record.SessionID {
		t.Fatalf("initialize stream = %+v, notifications=%d, %v", result, seen, err)
	}
}

func TestNATSBrokerAffinityRejectsMissingAndDeadSessions(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}
	registerTestConnector(t, b, "a", channels)
	if err := b.Enqueue(t.Context(), "tunnel", testQueuedCommand("without-session")); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("uninitialized request = %v", err)
	}
	sessionID := initializeAffinity(t, b, "a", "first", "")
	if err := b.updateControl(t.Context(), "tunnel", func(control *tunnelControl) error {
		delete(control.Channels["main"].Instances, "a")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registerTestConnector(t, b, "b", channels)
	old := testQueuedCommand("old-session")
	old.Headers.Set("Mcp-Session-Id", sessionID)
	if err := b.Enqueue(t.Context(), "tunnel", old); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("old session migrated to a new process: %v", err)
	}
	newID := initializeAffinity(t, b, "b", "second", "")
	if newID == sessionID {
		t.Fatal("reinitialization reused the old session")
	}
}

func TestNATSBrokerAffinityMapsNativeSessionAndStartsBeforeVisibility(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}
	registerTestConnector(t, b, "a", channels)
	registerTestConnector(t, b, "b", channels)
	sessionID := initializeAffinity(t, b, "a", "initialize", "private-session")
	command := testQueuedCommand("tools-list")
	command.Headers.Set("Mcp-Session-Id", sessionID)
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, "b", channels, 1); len(commands) != 0 {
		t.Fatal("affine command delivered to another process")
	}
	commands := pollTestCommands(t, b, "a", channels, 1)
	if len(commands) != 1 || commands[0].Headers.Get("Mcp-Session-Id") != "private-session" {
		t.Fatalf("downstream mapping = %+v", commands)
	}
}

func TestNATSBrokerStdioCloseDoesNotSendInvalidCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}
	registerTestConnector(t, b, "a", channels)
	sessionID := initializeAffinity(t, b, "a", "initialize", "")
	command := testQueuedCommand("close")
	command.CommandType, command.JSONRPC = CommandTypeSessionTermination, nil
	command.Headers.Set("Mcp-Session-Id", sessionID)
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	response, state, err := b.GetResponse(t.Context(), "tunnel", command.RequestID)
	if err != nil || state != "completed" || response.ResponseCode != http.StatusNoContent {
		t.Fatalf("close result = %+v, %s, %v", response, state, err)
	}
	if commands := pollTestCommands(t, b, "a", channels, 1); len(commands) != 0 {
		t.Fatal("stdio close incorrectly sent to connector without a downstream session")
	}
	command = testQueuedCommand("after-close")
	command.Headers.Set("Mcp-Session-Id", sessionID)
	if err := b.Enqueue(t.Context(), "tunnel", command); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("closed session accepted: %v", err)
	}
}

func initializeAffinity(t *testing.T, b *Broker, instance, requestID, upstreamID string) string {
	t.Helper()
	command := testQueuedCommand(requestID)
	command.JSONRPC = json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, b, instance, []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}, 1)
	if len(commands) != 1 || commands[0].Headers.Get("Mcp-Session-Id") != "" {
		t.Fatalf("initialize dispatch = %+v", commands)
	}
	response := testTerminalResponse(requestID)
	response.ResponseHeaders = http.Header{}
	if upstreamID != "" {
		response.ResponseHeaders.Set("Mcp-Session-Id", upstreamID)
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", instance, 1, commands[0].ShardToken, response); err != nil {
		t.Fatal(err)
	}
	stored, _, err := b.GetResponse(t.Context(), "tunnel", requestID)
	if err != nil {
		t.Fatal(err)
	}
	id := stored.ResponseHeaders.Get("Mcp-Session-Id")
	session, _, err := b.readSession(t.Context(), "tunnel", id)
	if err != nil || !session.Ready || session.InstanceID != instance || session.UpstreamID != upstreamID {
		t.Fatalf("visible result lacks committed ownership: %+v, %v", session, err)
	}
	return id
}
