package tunnels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/redis/go-redis/v9"
)

func TestBrokerLifecycleAndProcessAffinity(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 1, 2, 3, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL:        time.Minute,
		TombstoneTTL:       5 * time.Minute,
		MaxPendingRequests: 256,
		MaxPendingBytes:    32 << 20,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "11111111-1111-4111-8111-111111111111"
	declarations := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}

	first := testQueuedCommand("req_first", clock, "")
	if err := broker.Enqueue(ctx, tunnelUUID, first); !errors.Is(err, ErrNoConnector) {
		t.Fatalf("Enqueue without connector = %v, want ErrNoConnector", err)
	}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance-a", 1, declarations); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}
	if err := broker.Enqueue(ctx, tunnelUUID, first); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := broker.Claim(ctx, tunnelUUID, "instance-a", 1, declarations, 25)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].RequestID != first.RequestID || claimed[0].ShardToken == "" {
		t.Fatalf("Claim() = %+v", claimed)
	}
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance-a", 1, "wrong", testTerminalResponse(first.RequestID, "session-1")); !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("SubmitResponse wrong shard = %v, want ErrResponseMismatch", err)
	}
	wrongType := TunnelResponse{
		RequestID: first.RequestID, Channel: "main",
		ResponseType: ResponseTypeSessionTermination,
	}
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance-a", 1, claimed[0].ShardToken, wrongType); !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("SubmitResponse wrong response type = %v, want ErrResponseMismatch", err)
	}
	notification := TunnelResponse{
		RequestID: first.RequestID, Channel: "main", ResponseType: ResponseTypeJSONRPCNotify,
		JSONResponse: json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/progress"}`),
	}
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance-a", 1, claimed[0].ShardToken, notification); err != nil {
		t.Fatalf("SubmitResponse notification: %v", err)
	}
	if response, state, err := broker.GetResponse(ctx, tunnelUUID, first.RequestID); err != nil || state != "dispatched" || response != nil {
		t.Fatalf("GetResponse after notification = (%+v, %q, %v)", response, state, err)
	}
	if err := broker.SuspendTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("SuspendTokenVersion: %v", err)
	}
	if _, err := broker.Claim(ctx, tunnelUUID, "instance-a", 1, declarations, 25); !errors.Is(err, ErrTokenRetired) {
		t.Fatalf("Claim with retired token = %v, want ErrTokenRetired", err)
	}
	terminal := testTerminalResponse(first.RequestID, "session-1")
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance-a", 1, claimed[0].ShardToken, terminal); err != nil {
		t.Fatalf("SubmitResponse terminal with retired token: %v", err)
	}
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance-a", 1, claimed[0].ShardToken, terminal); err != nil {
		t.Fatalf("SubmitResponse duplicate terminal: %v", err)
	}
	response, state, err := broker.GetResponse(ctx, tunnelUUID, first.RequestID)
	if err != nil || state != "completed" || response == nil || response.ResponseCode != http.StatusOK {
		t.Fatalf("GetResponse terminal = (%+v, %q, %v)", response, state, err)
	}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 2); err != nil {
		t.Fatalf("ensureActiveTokenVersion repairs suspended prior version: %v", err)
	}
	if err := broker.ActivateTokenVersion(ctx, tunnelUUID, 1); !errors.Is(err, ErrTokenRetired) {
		t.Fatalf("ActivateTokenVersion stale version = %v, want ErrTokenRetired", err)
	}
	if commands, err := broker.Claim(ctx, tunnelUUID, "instance-a", 2, declarations, 25); err != nil || len(commands) != 0 {
		t.Fatalf("register rotated token connector = (%+v, %v)", commands, err)
	}

	second := testQueuedCommand("req_second", clock, "session-1")
	if err := broker.Enqueue(ctx, tunnelUUID, second); err != nil {
		t.Fatalf("Enqueue affinity request: %v", err)
	}
	if commands, err := broker.Claim(ctx, tunnelUUID, "instance-b", 2, declarations, 25); err != nil || len(commands) != 0 {
		t.Fatalf("Claim from non-owner = (%+v, %v), want empty", commands, err)
	}
	commands, err := broker.Claim(ctx, tunnelUUID, "instance-a", 2, declarations, 25)
	if err != nil || len(commands) != 1 || commands[0].RequestID != second.RequestID {
		t.Fatalf("Claim from affinity owner = (%+v, %v)", commands, err)
	}
	clock = clock.Add(2 * time.Minute)
	if err := broker.cleanup(ctx, tunnelUUID); err != nil {
		t.Fatalf("cleanup expired affinity: %v", err)
	}
	if count := client.HLen(ctx, broker.affinityKey(tunnelUUID)).Val(); count != 0 {
		t.Fatalf("expired affinity owner count = %d, want 0", count)
	}
	if count := client.ZCard(ctx, broker.affinityExpiryKey(tunnelUUID)).Val(); count != 0 {
		t.Fatalf("expired affinity index count = %d, want 0", count)
	}
}

func TestBrokerCancelAndExpiry(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 2, 0, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL: time.Minute, TombstoneTTL: 5 * time.Minute,
		MaxPendingRequests: 1, MaxPendingBytes: 1024,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "22222222-2222-4222-8222-222222222222"
	declarations := []ChannelDeclaration{{Name: "main"}}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance", 1, declarations); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}
	canceled := testQueuedCommand("req_canceled", clock, "")
	if err := broker.Enqueue(ctx, tunnelUUID, canceled); err != nil {
		t.Fatalf("Enqueue canceled: %v", err)
	}
	if err := broker.Cancel(ctx, tunnelUUID, canceled.RequestID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, state, err := broker.GetResponse(ctx, tunnelUUID, canceled.RequestID); err != nil || state != "canceled" {
		t.Fatalf("GetResponse canceled = (%q, %v)", state, err)
	}
	assertTerminalStateCompacted(t, ctx, client, broker.requestsKey(tunnelUUID), canceled.RequestID, false)

	expired := testQueuedCommand("req_expired", clock, "")
	if err := broker.Enqueue(ctx, tunnelUUID, expired); err != nil {
		t.Fatalf("Enqueue expired: %v", err)
	}
	clock = clock.Add(2 * time.Minute)
	if _, state, err := broker.GetResponse(ctx, tunnelUUID, expired.RequestID); err != nil || state != "expired" {
		t.Fatalf("GetResponse expired = (%q, %v)", state, err)
	}
	assertTerminalStateCompacted(t, ctx, client, broker.requestsKey(tunnelUUID), expired.RequestID, false)
}

func TestBrokerRejectsInvalidAndDuplicateChannelsAtBoundary(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	broker := NewBroker(client, config.TunnelConfig{})
	if err := broker.RegisterConnector(context.Background(), "tunnel", "instance", 1, []ChannelDeclaration{{Name: "../escape"}}); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("RegisterConnector invalid channel = %v, want ErrChannelInvalid", err)
	}
	if err := broker.RegisterConnector(context.Background(), "tunnel", "instance", 1, []ChannelDeclaration{{Name: "main"}, {Name: "main"}}); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("RegisterConnector duplicate channel = %v, want ErrChannelInvalid", err)
	}
	command := testQueuedCommand("req_invalid", time.Now(), "")
	command.Channel = "../escape"
	if err := broker.Enqueue(context.Background(), "tunnel", command); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("Enqueue invalid channel = %v, want ErrChannelInvalid", err)
	}
}

func TestBrokerSubscriptionBeforeEnqueuePreservesFastNotification(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 3, 0, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL: time.Minute, TombstoneTTL: 5 * time.Minute,
		MaxPendingRequests: 8, MaxPendingBytes: 4096,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "33333333-3333-4333-8333-333333333333"
	declarations := []ChannelDeclaration{{Name: "main"}}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance", 1, declarations); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}
	command := testQueuedCommand("req_fast_notification", clock, "")
	waiter, err := broker.subscribeResponse(ctx, tunnelUUID, command.RequestID, true)
	if err != nil {
		t.Fatalf("subscribeResponse: %v", err)
	}
	defer waiter.Close()
	if err := broker.Enqueue(ctx, tunnelUUID, command); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := broker.Claim(ctx, tunnelUUID, "instance", 1, declarations, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim = (%+v, %v)", claimed, err)
	}
	notification := TunnelResponse{
		RequestID: command.RequestID, Channel: "main", ResponseType: ResponseTypeJSONRPCNotify,
		JSONResponse: json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/progress"}`),
	}
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance", 1, claimed[0].ShardToken, notification); err != nil {
		t.Fatalf("SubmitResponse notification: %v", err)
	}
	terminal := testTerminalResponse(command.RequestID, "")
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance", 1, claimed[0].ShardToken, terminal); err != nil {
		t.Fatalf("SubmitResponse terminal: %v", err)
	}
	assertTerminalStateCompacted(t, ctx, client, broker.requestsKey(tunnelUUID), command.RequestID, true)
	var notifications []TunnelResponse
	response, err := waiter.Wait(ctx, func(value TunnelResponse) {
		notifications = append(notifications, value)
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(notifications) != 1 || notifications[0].ResponseType != ResponseTypeJSONRPCNotify {
		t.Fatalf("notifications = %+v", notifications)
	}
	if response.ResponseType != ResponseTypeJSONRPC {
		t.Fatalf("terminal response = %+v", response)
	}
	assertTerminalStateCompacted(t, ctx, client, broker.requestsKey(tunnelUUID), command.RequestID, false)
}

func TestBrokerPreEnqueueWaiterFallsBackToDurableTerminal(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 3, 15, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL: time.Minute, TombstoneTTL: 5 * time.Minute,
		MaxPendingRequests: 8, MaxPendingBytes: 4096,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "33333333-3333-4333-8333-333333333336"
	declarations := []ChannelDeclaration{{Name: "main"}}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance", 1, declarations); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}
	command := testQueuedCommand("req_durable_fallback", clock, "")
	if err := broker.Enqueue(ctx, tunnelUUID, command); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := broker.Claim(ctx, tunnelUUID, "instance", 1, declarations, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim = (%+v, %v)", claimed, err)
	}
	terminal := testTerminalResponse(command.RequestID, "")
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance", 1, claimed[0].ShardToken, terminal); err != nil {
		t.Fatalf("SubmitResponse terminal: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	waiter := &responseWaiter{
		broker: broker,
		subscription: &responseSubscription{
			deliveries: make(chan responseDelivery),
		},
		tunnelUUID: tunnelUUID,
		requestID:  command.RequestID,
		preEnqueue: true,
	}
	response, err := waiter.Wait(waitCtx, nil)
	if err != nil || response.ResponseType != ResponseTypeJSONRPC {
		t.Fatalf("Wait with lost Pub/Sub terminal = (%+v, %v)", response, err)
	}
	assertTerminalStateCompacted(t, ctx, client, broker.requestsKey(tunnelUUID), command.RequestID, false)
}

func TestBrokerWaiterReturnsDurableTerminalDuringReconnectWindow(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 3, 30, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL: time.Minute, TombstoneTTL: 5 * time.Minute,
		MaxPendingRequests: 8, MaxPendingBytes: 4096,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "33333333-3333-4333-8333-333333333334"
	declarations := []ChannelDeclaration{{Name: "main"}}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance", 1, declarations); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}
	command := testQueuedCommand("req_durable_terminal", clock, "")
	if err := broker.Enqueue(ctx, tunnelUUID, command); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := broker.Claim(ctx, tunnelUUID, "instance", 1, declarations, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim = (%+v, %v)", claimed, err)
	}

	secondRead := make(chan struct{}, 1)
	releaseSecondRead := make(chan struct{})
	client.AddHook(&blockHGetHook{
		key: broker.requestsKey(tunnelUUID), field: command.RequestID,
		blockAt: 2, blocked: secondRead, release: releaseSecondRead,
	})
	deliveries := make(chan responseDelivery)
	waiter := &responseWaiter{
		broker: broker,
		subscription: &responseSubscription{
			deliveries: deliveries,
		},
		tunnelUUID: tunnelUUID,
		requestID:  command.RequestID,
	}
	type waitResult struct {
		response TunnelResponse
		err      error
	}
	result := make(chan waitResult, 1)
	go func() {
		response, waitErr := waiter.Wait(ctx, nil)
		result <- waitResult{response: response, err: waitErr}
	}()
	select {
	case deliveries <- responseDelivery{err: errors.New("simulated Pub/Sub failure")}:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not receive the simulated Pub/Sub failure")
	}
	select {
	case <-secondRead:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not inspect durable state before reconnecting")
	}
	terminal := testTerminalResponse(command.RequestID, "")
	if err := broker.SubmitResponse(ctx, tunnelUUID, "instance", 1, claimed[0].ShardToken, terminal); err != nil {
		t.Fatalf("SubmitResponse terminal: %v", err)
	}
	close(releaseSecondRead)
	select {
	case got := <-result:
		if got.err != nil || got.response.ResponseType != ResponseTypeJSONRPC {
			t.Fatalf("Wait after Pub/Sub failure = (%+v, %v)", got.response, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not recover the durable terminal response")
	}
	assertTerminalStateCompacted(t, ctx, client, broker.requestsKey(tunnelUUID), command.RequestID, false)
}

func TestBrokerCleanupDrainsMoreThanOneBatch(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 3, 45, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL: time.Minute, TombstoneTTL: 5 * time.Minute,
		MaxPendingRequests: config.MaxTunnelPendingRequests, MaxPendingBytes: 1 << 20,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "33333333-3333-4333-8333-333333333335"
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance", 1, []ChannelDeclaration{{Name: "main"}}); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}

	const expiredRequestCount = brokerCleanupBatchSize + 88
	pipeline := client.Pipeline()
	for index := range expiredRequestCount {
		requestID := fmt.Sprintf("req_expired_%03d", index)
		command := testQueuedCommand(requestID, clock.Add(-2*time.Minute), "")
		command.ExpiresAtMS = command.ExpiresAt.UnixMilli()
		encoded, err := json.Marshal(brokerRequestState{queuedCommand: command, State: "queued"})
		if err != nil {
			t.Fatalf("encode request state %d: %v", index, err)
		}
		pipeline.HSet(ctx, broker.requestsKey(tunnelUUID), requestID, encoded)
		pipeline.ZAdd(ctx, broker.queueKey(tunnelUUID, "main"), redis.Z{Score: float64(index), Member: requestID})
		pipeline.ZAdd(ctx, broker.expiryKey(tunnelUUID), redis.Z{Score: float64(command.ExpiresAtMS), Member: requestID})
	}
	pipeline.HSet(ctx, broker.budgetKey(tunnelUUID),
		"pending_count", expiredRequestCount,
		"pending_bytes", expiredRequestCount*int(testQueuedCommand("sample", clock, "").PayloadSize),
	)
	if _, err := pipeline.Exec(ctx); err != nil {
		t.Fatalf("seed expired requests: %v", err)
	}
	if err := broker.cleanup(ctx, tunnelUUID); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	budget, err := client.HMGet(ctx, broker.budgetKey(tunnelUUID), "pending_count", "pending_bytes").Result()
	if err != nil {
		t.Fatalf("read budget: %v", err)
	}
	if fmt.Sprint(budget[0]) != "0" || fmt.Sprint(budget[1]) != "0" {
		t.Fatalf("budget after cleanup = %v, want [0 0]", budget)
	}
	if remaining, err := client.ZCard(ctx, broker.queueKey(tunnelUUID, "main")).Result(); err != nil || remaining != 0 {
		t.Fatalf("queued requests after cleanup = (%d, %v), want 0", remaining, err)
	}
	if err := broker.Enqueue(ctx, tunnelUUID, testQueuedCommand("req_after_cleanup", clock, "")); err != nil {
		t.Fatalf("Enqueue after cleanup: %v", err)
	}
}

func TestBrokerResponseWaitersShareOnePubSubConnection(t *testing.T) {
	client := startTestRedis(t)
	broker := NewBroker(client, config.TunnelConfig{})
	ctx := context.Background()
	first, err := broker.subscribeResponse(ctx, "44444444-4444-4444-8444-444444444441", "req_first", true)
	if err != nil {
		t.Fatalf("subscribe first response: %v", err)
	}
	defer first.Close()
	second, err := broker.subscribeResponse(ctx, "44444444-4444-4444-8444-444444444442", "req_second", true)
	if err != nil {
		t.Fatalf("subscribe second response: %v", err)
	}
	defer second.Close()
	if active := client.PoolStats().PubSubStats.Active; active != 1 {
		t.Fatalf("active Pub/Sub connections = %d, want 1", active)
	}
}

func TestBrokerClaimDoesNotStarveRunnableRequestBehindAffinityQueue(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 20, 4, 0, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{
		PresenceTTL: time.Minute, TombstoneTTL: 5 * time.Minute,
		MaxPendingRequests: 70, MaxPendingBytes: 1 << 20,
	})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "44444444-4444-4444-8444-444444444440"
	declarations := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "other-instance", 1, declarations); err != nil {
		t.Fatalf("RegisterConnector: %v", err)
	}
	for index := range 64 {
		affinityKey := fmt.Sprintf("session-%d", index)
		ownerKey := "main:" + affinityKey
		owner := fmt.Sprintf(`{"instance_id":"owner-instance","expires_at":%d}`, clock.Add(time.Minute).UnixMilli())
		if err := client.HSet(ctx, broker.affinityKey(tunnelUUID), ownerKey, owner).Err(); err != nil {
			t.Fatalf("seed affinity owner: %v", err)
		}
		if err := client.ZAdd(ctx, broker.affinityExpiryKey(tunnelUUID), redis.Z{
			Score: float64(clock.Add(time.Minute).UnixMilli()), Member: ownerKey,
		}).Err(); err != nil {
			t.Fatalf("seed affinity expiry: %v", err)
		}
		if err := broker.Enqueue(ctx, tunnelUUID, testQueuedCommand(fmt.Sprintf("req_blocked_%02d", index), clock, affinityKey)); err != nil {
			t.Fatalf("Enqueue affinity request %d: %v", index, err)
		}
	}
	runnable := testQueuedCommand("req_runnable", clock, "")
	if err := broker.Enqueue(ctx, tunnelUUID, runnable); err != nil {
		t.Fatalf("Enqueue runnable request: %v", err)
	}
	claimed, err := broker.Claim(ctx, tunnelUUID, "other-instance", 1, declarations, 1)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].RequestID != runnable.RequestID {
		t.Fatalf("Claim = %+v, want runnable request behind affinity queue", claimed)
	}
}

func assertTerminalStateCompacted(
	t *testing.T,
	ctx context.Context,
	client *redis.Client,
	requestsKey string,
	requestID string,
	wantResponse bool,
) {
	t.Helper()
	raw, err := client.HGet(ctx, requestsKey, requestID).Bytes()
	if err != nil {
		t.Fatalf("read terminal request state: %v", err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode terminal request state: %v", err)
	}
	for _, field := range []string{"created_at", "headers", "jsonrpc", "expires_at", "expires_at_unix_ms", "payload_size", "affinity_key"} {
		if _, exists := state[field]; exists {
			t.Fatalf("terminal request state retained %q: %s", field, raw)
		}
	}
	_, hasResponse := state["response"]
	if hasResponse != wantResponse {
		t.Fatalf("terminal request response present = %t, want %t: %s", hasResponse, wantResponse, raw)
	}
}

func TestBrokerConnectorSnapshotsTrackChannelsInstancesAndExpiry(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 21, 1, 0, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{PresenceTTL: time.Minute})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "44444444-4444-4444-8444-444444444444"
	channels := []ChannelDeclaration{{Name: "main", ProcessAffinity: true}, {Name: "aux"}}
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	for _, instanceID := range []string{"instance-a", "instance-b"} {
		if err := broker.RegisterConnector(ctx, tunnelUUID, instanceID, 1, channels); err != nil {
			t.Fatalf("RegisterConnector(%s): %v", instanceID, err)
		}
	}

	snapshot, err := broker.ConnectorSnapshot(ctx, tunnelUUID)
	if err != nil {
		t.Fatalf("ConnectorSnapshot: %v", err)
	}
	if snapshot.State != "connected" || snapshot.InstanceCount != 2 || len(snapshot.Channels) != 2 {
		t.Fatalf("connected snapshot = %+v", snapshot)
	}
	if snapshot.Channels[0].Name != "aux" || snapshot.Channels[0].ProcessAffinity || snapshot.Channels[0].InstanceCount != 2 {
		t.Fatalf("aux snapshot = %+v", snapshot.Channels[0])
	}
	if snapshot.Channels[1].Name != "main" || !snapshot.Channels[1].ProcessAffinity || snapshot.Channels[1].InstanceCount != 2 {
		t.Fatalf("main snapshot = %+v", snapshot.Channels[1])
	}

	clock = clock.Add(2 * time.Minute)
	snapshot, err = broker.ConnectorSnapshot(ctx, tunnelUUID)
	if err != nil {
		t.Fatalf("ConnectorSnapshot expired: %v", err)
	}
	if snapshot.State != "disconnected" || snapshot.InstanceCount != 0 || len(snapshot.Channels) != 0 {
		t.Fatalf("expired snapshot = %+v", snapshot)
	}
}

func TestBrokerRegistrationPrunesExpiredChannelDeclarations(t *testing.T) {
	client := startTestRedis(t)
	clock := time.Date(2026, time.August, 21, 1, 30, 0, 0, time.UTC)
	broker := NewBroker(client, config.TunnelConfig{PresenceTTL: time.Minute})
	broker.now = func() time.Time { return clock }
	ctx := context.Background()
	tunnelUUID := "44444444-4444-4444-8444-444444444445"
	if err := broker.ensureActiveTokenVersion(ctx, tunnelUUID, 1); err != nil {
		t.Fatalf("ensureActiveTokenVersion: %v", err)
	}
	channels := make([]ChannelDeclaration, 0, maxTunnelChannels)
	for index := range maxTunnelChannels {
		channels = append(channels, ChannelDeclaration{Name: fmt.Sprintf("channel_%02d", index)})
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance-a", 1, channels); err != nil {
		t.Fatalf("RegisterConnector initial channels: %v", err)
	}
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance-b", 1, []ChannelDeclaration{{Name: "channel_00", ProcessAffinity: true}}); !errors.Is(err, ErrChannelMismatch) {
		t.Fatalf("RegisterConnector conflicting live channel = %v, want ErrChannelMismatch", err)
	}

	clock = clock.Add(2 * time.Minute)
	if err := broker.RegisterConnector(ctx, tunnelUUID, "instance-b", 1, []ChannelDeclaration{{Name: "fresh", ProcessAffinity: true}}); err != nil {
		t.Fatalf("RegisterConnector after declarations expired: %v", err)
	}
	snapshot, err := broker.ConnectorSnapshot(ctx, tunnelUUID)
	if err != nil {
		t.Fatalf("ConnectorSnapshot: %v", err)
	}
	if snapshot.State != "connected" || snapshot.InstanceCount != 1 || len(snapshot.Channels) != 1 {
		t.Fatalf("snapshot after declaration pruning = %+v", snapshot)
	}
	if snapshot.Channels[0].Name != "fresh" || !snapshot.Channels[0].ProcessAffinity {
		t.Fatalf("fresh channel snapshot = %+v", snapshot.Channels[0])
	}
}

func TestConsoleConnectorSnapshotsDegradeRedisFailureToUnknown(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	if err := client.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
	handler := &ConsoleHandler{
		broker: NewBroker(client, config.TunnelConfig{PresenceTTL: time.Minute}),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	records := []db.MCPTunnel{{UUID: "55555555-5555-4555-8555-555555555555"}}
	snapshots := handler.connectorSnapshots(httptest.NewRequest(http.MethodGet, "/", nil), records)
	snapshot := snapshots[records[0].UUID]
	if snapshot.State != "unknown" || snapshot.InstanceCount != 0 || len(snapshot.Channels) != 0 {
		t.Fatalf("degraded snapshot = %+v, want unknown", snapshot)
	}
}

func testQueuedCommand(requestID string, now time.Time, affinityKey string) queuedCommand {
	payload := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	return queuedCommand{
		RequestID: requestID, CommandType: CommandTypeJSONRPC, Channel: "main",
		CreatedAt: now, Headers: http.Header{}, JSONRPC: payload,
		ExpiresAt: now.Add(time.Minute), PayloadSize: int64(len(payload)), AffinityKey: affinityKey,
	}
}

func testTerminalResponse(requestID, sessionID string) TunnelResponse {
	return TunnelResponse{
		RequestID: requestID, Channel: "main", ResponseCode: http.StatusOK,
		ResponseType:    ResponseTypeJSONRPC,
		JSONResponse:    json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`),
		ResponseHeaders: http.Header{"Mcp-Session-Id": []string{sessionID}},
	}
}

func startTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	path, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Redis port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	var logs bytes.Buffer
	command := exec.Command(path,
		"--bind", "127.0.0.1", "--port", strconv.Itoa(port),
		"--protected-mode", "no", "--save", "", "--appendonly", "no",
		"--dir", t.TempDir(),
	)
	command.Stdout = &logs
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatalf("start redis-server: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port)})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if err := client.Ping(ctx).Err(); err == nil {
			return client
		}
		select {
		case <-ctx.Done():
			t.Fatalf("redis-server did not start: %s", logs.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type blockHGetHook struct {
	key     string
	field   string
	blockAt int
	blocked chan<- struct{}
	release <-chan struct{}
	count   int
}

func (h *blockHGetHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *blockHGetHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		err := next(ctx, command)
		args := command.Args()
		if command.Name() == "hget" && len(args) == 3 && fmt.Sprint(args[1]) == h.key && fmt.Sprint(args[2]) == h.field {
			h.count++
			if h.count == h.blockAt {
				select {
				case h.blocked <- struct{}{}:
				default:
				}
				select {
				case <-h.release:
				case <-ctx.Done():
				}
			}
		}
		return err
	}
}

func (h *blockHGetHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
