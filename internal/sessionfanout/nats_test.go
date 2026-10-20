package sessionfanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestNATSBusRejectsInvalidInput(t *testing.T) {
	if _, err := NewNATS(t.Context(), nil, nil); !errors.Is(err, nats.ErrDisconnected) {
		t.Fatalf("NewNATS(nil) = %v", err)
	}
	srv := startFanoutNATS(t)
	b, _ := connectFanoutNATS(t, srv.ClientURL())
	for _, sessionID := range []string{"", "*", ">", "a.b", "a b", "a\n", "a\t", "a\x00"} {
		t.Run(fmt.Sprintf("invalid session %q", sessionID), func(t *testing.T) {
			for _, err := range []error{
				b.Subscribe(t.Context(), sessionID),
				b.Unsubscribe(sessionID),
				b.Publish(t.Context(), sessionID, fanoutEnvelope("test")),
			} {
				if err == nil {
					t.Fatal("accepted invalid session ID")
				}
			}
		})
	}
	for _, envelope := range []Envelope{
		{Kind: "unknown", Payload: json.RawMessage(`{}`)},
		{Kind: KindSessionEvents},
		{Kind: KindSessionEvents, Payload: json.RawMessage(`invalid`)},
	} {
		if err := b.Publish(t.Context(), "session-test", envelope); err == nil {
			t.Fatal("accepted invalid envelope")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := b.Subscribe(ctx, "session-test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Subscribe(canceled) = %v", err)
	}
	if err := b.Publish(ctx, "session-test", fanoutEnvelope("test")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish(canceled) = %v", err)
	}
}

func TestNATSBusCloseReleasesOnlyOwnedSubscriptions(t *testing.T) {
	srv := startFanoutNATS(t)
	b, connection := connectFanoutNATS(t, srv.ClientURL())
	unrelated, err := connection.SubscribeSync("another.component")
	if err != nil {
		t.Fatal(err)
	}
	defer unrelated.Unsubscribe()
	if err := b.Subscribe(t.Context(), "session-test"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !connection.IsConnected() || connection.NumSubscriptions() != 1 {
		t.Fatal("Close changed the shared connection or unrelated subscription")
	}
	if err := b.Subscribe(t.Context(), "session-test"); !errors.Is(err, errNATSBusClosed) {
		t.Fatalf("Subscribe after Close = %v", err)
	}
	if err := b.Publish(t.Context(), "session-test", fanoutEnvelope("test")); !errors.Is(err, errNATSBusClosed) {
		t.Fatalf("Publish after Close = %v", err)
	}
}

func TestNATSBusSubscriptionConfirmationDoesNotHoldRegistryLock(t *testing.T) {
	srv := startFanoutNATS(t)
	b, _ := connectFanoutNATS(t, srv.ClientURL())
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	b.flush = func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	subscribed := make(chan error, 1)
	go func() {
		subscribed <- b.Subscribe(t.Context(), "session-blocked")
	}()
	awaitNATSSignal(t, started)

	lockAcquired := make(chan struct{})
	go func() {
		b.mu.Lock()
		b.mu.Unlock()
		close(lockAcquired)
	}()
	awaitNATSSignal(t, lockAcquired)

	publishCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := b.Publish(publishCtx, "session-other", fanoutEnvelope("while-confirming")); err != nil {
		t.Fatalf("Publish() while subscription confirmation is blocked: %v", err)
	}

	releaseOnce.Do(func() { close(release) })
	if err := <-subscribed; err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
}

func TestNATSBusConcurrentSubscribersShareConfirmation(t *testing.T) {
	srv := startFanoutNATS(t)
	b, _ := connectFanoutNATS(t, srv.ClientURL())
	started := make(chan struct{})
	release := make(chan struct{})
	confirmations := make(chan struct{}, 10)
	var startOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	b.flush = func(ctx context.Context) error {
		confirmations <- struct{}{}
		startOnce.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}

	const subscriberCount = 8
	errorsBySubscriber := make(chan error, subscriberCount)
	var subscribers sync.WaitGroup
	for range subscriberCount {
		subscribers.Go(func() {
			errorsBySubscriber <- b.Subscribe(t.Context(), "session-shared")
		})
	}
	awaitNATSSignal(t, started)
	releaseOnce.Do(func() { close(release) })
	subscribers.Wait()
	close(errorsBySubscriber)
	for err := range errorsBySubscriber {
		if err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}
	}
	if got := len(confirmations); got != 1 {
		t.Fatalf("subscription confirmations = %d, want 1", got)
	}
}

func TestNATSBusCloseUnblocksSubscriptionConfirmation(t *testing.T) {
	srv := startFanoutNATS(t)
	b, _ := connectFanoutNATS(t, srv.ClientURL())
	started := make(chan struct{})
	b.flush = func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	subscribed := make(chan error, 1)
	go func() {
		subscribed <- b.Subscribe(t.Context(), "session-close")
	}()
	awaitNATSSignal(t, started)

	closed := make(chan error, 1)
	go func() {
		closed <- b.Close()
	}()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() blocked on subscription confirmation")
	}
	if err := <-subscribed; !errors.Is(err, errNATSBusClosed) {
		t.Fatalf("Subscribe() after Close = %v, want closed", err)
	}
}

func TestNATSBusDiscardsMalformedMessagesWithoutPayloadLogging(t *testing.T) {
	srv := startFanoutNATS(t)
	connection := connectFanoutNATSConnection(t, srv.ClientURL())
	var logs bytes.Buffer
	b, err := NewNATS(t.Context(), connection, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	received := recordNATSEnvelopes(b)
	if err := b.Subscribe(t.Context(), "session-test"); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{"secret":"sensitive-payload"`, `{"kind":"invalid","payload":{"secret":"sensitive-payload"}}`} {
		if err := connection.Publish("oma.s.session-test", []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Publish(t.Context(), "session-test", fanoutEnvelope("barrier")); err != nil {
		t.Fatal(err)
	}
	assertNATSEnvelope(t, received, "barrier")
	_ = b.Close()
	if strings.Contains(logs.String(), "sensitive-payload") {
		t.Fatal("message payload leaked into logs")
	}
	decoder := json.NewDecoder(&logs)
	for range 2 {
		var record struct {
			Level     string `json:"level"`
			Message   string `json:"msg"`
			SessionID string `json:"session_id"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Level != "WARN" || record.SessionID != "session-test" || !strings.HasPrefix(record.Message, "discard ") {
			t.Fatalf("unexpected log record: %+v", record)
		}
	}
}

func TestNATSBusReconnectResetsAndRestoresSubscriptions(t *testing.T) {
	srv := startFanoutNATS(t)
	url := srv.ClientURL()
	port := srv.Addr().(*net.TCPAddr).Port
	b, connection := connectFanoutNATS(t, url)
	received := recordNATSEnvelopes(b)
	resets := make(chan struct{}, 10)
	b.Register(func(context.Context, string, Envelope) {}, func(context.Context, string) { resets <- struct{}{} })
	if err := b.Subscribe(t.Context(), "session-test"); err != nil {
		t.Fatal(err)
	}
	srv.Shutdown()
	srv.WaitForShutdown()
	waitNATSCondition(t, connection.IsReconnecting)
	awaitNATSSignal(t, resets)
	if err := b.Publish(t.Context(), "session-test", fanoutEnvelope("offline")); err == nil {
		t.Fatal("offline publication was accepted")
	}
	restarted, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	restarted.Start()
	t.Cleanup(restarted.Shutdown)
	if !restarted.ReadyForConnections(3 * time.Second) {
		t.Fatal("restarted server not ready")
	}
	waitNATSCondition(t, connection.IsConnected)
	if err := b.Publish(t.Context(), "session-test", fanoutEnvelope("after-reconnect")); err != nil {
		t.Fatal(err)
	}
	assertNATSEnvelope(t, received, "after-reconnect")
	assertNoNATSSignal(t, resets)
}

func TestNATSBusBroadcastsInOrderAndSharesSessionSubscriptions(t *testing.T) {
	srv := startFanoutNATS(t)
	publisher, _ := connectFanoutNATS(t, srv.ClientURL())
	first, firstConnection := connectFanoutNATS(t, srv.ClientURL())
	second, _ := connectFanoutNATS(t, srv.ClientURL())
	firstEvents, secondEvents := recordNATSEnvelopes(first), recordNATSEnvelopes(second)
	for _, b := range []*NATSBus{first, second} {
		var subscriptions sync.WaitGroup
		for range 8 {
			subscriptions.Go(func() {
				if err := b.Subscribe(t.Context(), "session-test"); err != nil {
					t.Error(err)
				}
			})
		}
		subscriptions.Wait()
	}
	if firstConnection.NumSubscriptions() != 1 {
		t.Fatalf("subscriptions = %d, want 1", firstConnection.NumSubscriptions())
	}
	if err := publisher.Publish(t.Context(), "session-other", fanoutEnvelope("unsubscribed")); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		value := fmt.Sprintf("event-%d", i)
		if err := publisher.Publish(t.Context(), "session-test", fanoutEnvelope(value)); err != nil {
			t.Fatal(err)
		}
		assertNATSEnvelope(t, firstEvents, value)
		assertNATSEnvelope(t, secondEvents, value)
	}
	if err := first.Unsubscribe("session-test"); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), "session-test", fanoutEnvelope("still-both")); err != nil {
		t.Fatal(err)
	}
	assertNATSEnvelope(t, firstEvents, "still-both")
	assertNATSEnvelope(t, secondEvents, "still-both")
	for range 7 {
		if err := first.Unsubscribe("session-test"); err != nil {
			t.Fatal(err)
		}
	}
	waitNATSCondition(t, func() bool { return firstConnection.NumSubscriptions() == 0 })
	if err := firstConnection.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := first.Unsubscribe("session-test"); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), "session-test", fanoutEnvelope("only-second")); err != nil {
		t.Fatal(err)
	}
	assertNATSEnvelope(t, secondEvents, "only-second")
	if err := first.Subscribe(t.Context(), "session-test"); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), "session-test", fanoutEnvelope("both-again")); err != nil {
		t.Fatal(err)
	}
	assertNATSEnvelope(t, firstEvents, "both-again")
	assertNATSEnvelope(t, secondEvents, "both-again")
}

func startFanoutNATS(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(3 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	return srv
}

func connectFanoutNATSConnection(t *testing.T, url string) *nats.Conn {
	t.Helper()
	connection, err := nats.Connect(url, nats.ReconnectWait(20*time.Millisecond), nats.ReconnectJitter(0, 0), nats.ReconnectBufSize(-1), nats.ErrorHandler(func(*nats.Conn, *nats.Subscription, error) {}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	return connection
}

func connectFanoutNATS(t *testing.T, url string) (*NATSBus, *nats.Conn) {
	t.Helper()
	connection := connectFanoutNATSConnection(t, url)
	b, err := NewNATS(t.Context(), connection, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, connection
}

func fanoutEnvelope(value string) Envelope {
	payload, _ := json.Marshal(value)
	return Envelope{Kind: KindSessionEvents, Payload: payload}
}

func recordNATSEnvelopes(b *NATSBus) <-chan Envelope {
	result := make(chan Envelope, 32)
	b.Register(func(_ context.Context, _ string, envelope Envelope) { result <- envelope }, nil)
	return result
}

func assertNATSEnvelope(t *testing.T, received <-chan Envelope, value string) {
	t.Helper()
	select {
	case envelope := <-received:
		if string(envelope.Payload) != string(fanoutEnvelope(value).Payload) {
			t.Fatalf("received %s, want %q", envelope.Payload, value)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("did not receive %q", value)
	}
}

func awaitNATSSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func assertNoNATSSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal("unexpected reset")
	case <-time.After(100 * time.Millisecond):
	}
}

func waitNATSCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}
