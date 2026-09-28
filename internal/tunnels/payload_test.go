package tunnels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

var payloadTestScope = payloadScope{OrganizationUUID: "52000000-0000-0000-0000-000000000001", WorkspaceUUID: "52000000-0000-0000-0000-000000000002"}

type payloadTestStorage struct {
	storage.ObjectStore
	mu          sync.Mutex
	bodies      map[string][]byte
	uploadErr   error
	unknownSize bool
}

func (s *payloadTestStorage) Name() string { return "test" }
func (s *payloadTestStorage) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
	if s.uploadErr != nil {
		return storage.UploadResult{}, s.uploadErr
	}
	data, err := io.ReadAll(r)
	s.mu.Lock()
	s.bodies[key] = data
	s.mu.Unlock()
	return storage.UploadResult{Size: int64(len(data))}, err
}
func (s *payloadTestStorage) Open(ctx context.Context, key string, _ *storage.ByteRange) (storage.Object, error) {
	if err := ctx.Err(); err != nil {
		return storage.Object{}, err
	}
	s.mu.Lock()
	data, ok := s.bodies[key]
	s.mu.Unlock()
	if !ok {
		return storage.Object{}, errors.New("object missing")
	}
	size := int64(len(data))
	if s.unknownSize {
		size = -1
	}
	return storage.Object{Body: io.NopCloser(bytes.NewReader(data)), Size: size}, nil
}

type payloadTestCleanup struct {
	count    int
	deadline time.Time
	err      error
}

func (q *payloadTestCleanup) EnqueueScheduledObjectCleanupResourceJob(_ context.Context, _, _, _, _, _, _ string, at time.Time) error {
	q.count++
	q.deadline = at
	return q.err
}
func newPayloadTestStore() (*PayloadStore, *payloadTestStorage, *payloadTestCleanup) {
	objects := &payloadTestStorage{bodies: make(map[string][]byte)}
	jobs := &payloadTestCleanup{}
	return &PayloadStore{objects: objects, cleanup: jobs}, objects, jobs
}
func sizedTunnelJSON(size int) json.RawMessage {
	return json.RawMessage(`{"text":"` + strings.Repeat("x", size-11) + `"}`)
}

func TestTunnelPayloadStorageFailures(t *testing.T) {
	for _, failure := range []string{"cleanup", "upload", "scope", "oversize", "expired"} {
		t.Run(failure, func(t *testing.T) {
			store, objects, jobs := newPayloadTestStore()
			scope, deadline, limit := payloadTestScope, time.Now().Add(time.Minute), int64(100)
			switch failure {
			case "cleanup":
				jobs.err = errors.New("database unavailable")
			case "upload":
				objects.uploadErr = errors.New("storage unavailable")
			case "scope":
				scope.WorkspaceUUID = ""
			case "oversize":
				limit = 2
			case "expired":
				deadline = time.Now().Add(-time.Minute)
			}
			if _, err := store.save(t.Context(), scope, "request", deadline, sizedTunnelJSON(20), limit); err == nil {
				t.Fatal("expected failure")
			}
			if len(objects.bodies) != 0 {
				t.Fatal("failure uploaded a body")
			}
		})
	}
	for _, failure := range []string{"missing", "size", "digest", "scope", "request", "both", "overread", "expired"} {
		t.Run(failure, func(t *testing.T) {
			store, objects, _ := newPayloadTestStore()
			ref, err := store.save(t.Context(), payloadTestScope, "request", time.Now().Add(time.Minute), sizedTunnelJSON(20), 100)
			if err != nil {
				t.Fatal(err)
			}
			scope, requestID := payloadTestScope, "request"
			var inline json.RawMessage
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch failure {
			case "missing":
				delete(objects.bodies, ref.Key)
			case "size":
				ref.Size++
			case "digest":
				ref.SHA256 = strings.Repeat("0", 64)
			case "scope":
				scope.WorkspaceUUID = scope.OrganizationUUID
			case "request":
				requestID = "other"
			case "both":
				inline = sizedTunnelJSON(20)
			case "overread":
				objects.unknownSize = true
				objects.bodies[ref.Key] = sizedTunnelJSON(30)
			case "expired":
				cancel()
			}
			if _, err := store.restore(ctx, scope, requestID, inline, ref, 100); err == nil {
				t.Fatal("expected restore failure")
			}
		})
	}
}

func TestTunnelPayloadNATSBoundary(t *testing.T) {
	for _, direction := range []string{"request", "response"} {
		for _, offset := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("%s/%+d", direction, offset), func(t *testing.T) {
				b := testNATSBroker(t, brokerTestConfig())
				b.cfg.MaxBodyBytes = 16 << 20
				store, _, jobs := newPayloadTestStore()
				b.payloads = store
				command := testQueuedCommand("boundary")
				command.Scope = payloadTestScope
				command.TunnelID, command.Origin = "tunnel", b.responseHub.subject
				command.Headers.Set("X-Test", "<escaped>&")
				if direction == "request" {
					command.JSONRPC = sizedTunnelJSON(11)
					base, _ := encodeTunnelJSON(command, 0)
					command.JSONRPC = sizedTunnelJSON(commandMessageLimit(command.RequestID) - len(base) + 11 + offset)
					if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
						t.Fatal(err)
					}
					messages := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
					if len(messages) != 1 || !bytes.Equal(messages[0].JSONRPC, command.JSONRPC) {
						t.Fatal("request not restored")
					}
				} else {
					response := testTerminalResponse(command.RequestID)
					response.JSONResponse = sizedTunnelJSON(11)
					base, _ := encodeTunnelJSON(responseEnvelope{Key: brokerKey(command.RequestID), Response: &response}, 0)
					response.JSONResponse = sizedTunnelJSON(maxBrokerValueBytes - len(base) + 11 + offset)
					data, err := b.encodeResponse(t.Context(), requestRecord{Scope: payloadTestScope, ExpiresAt: command.ExpiresAt}, response)
					if err != nil {
						t.Fatal(err)
					}
					if len(data) > maxBrokerValueBytes {
						t.Fatal("oversized NATS envelope")
					}
					var envelope responseEnvelope
					if err := json.Unmarshal(data, &envelope); err != nil {
						t.Fatal(err)
					}
					body, err := store.restore(t.Context(), envelope.Scope, command.RequestID, envelope.Response.JSONResponse, envelope.PayloadRef, b.cfg.MaxBodyBytes)
					if err != nil || !bytes.Equal(body, response.JSONResponse) {
						t.Fatalf("restore: %v", err)
					}
				}
				want := 0
				if offset > 0 {
					want = 1
				}
				if jobs.count != want {
					t.Fatalf("cleanup writes=%d want=%d", jobs.count, want)
				}
				if want == 1 && !jobs.deadline.Equal(command.ExpiresAt.Add(5*time.Minute)) {
					t.Fatal("cleanup deadline changed")
				}
			})
		}
	}
}

func TestTunnelPayloadCrossInstanceAndDuplicate(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.MaxBodyBytes = 16 << 20
	origin := testNATSBroker(t, cfg)
	peer, err := newBroker(t.Context(), connectTunnelNATS(t, origin.connection.ConnectedUrl()), cfg, 1, origin.requests)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	store, _, jobs := newPayloadTestStore()
	origin.payloads, peer.payloads = store, store
	command := testQueuedCommand("cross-instance")
	command.Scope = payloadTestScope
	command.JSONRPC = sizedTunnelJSON(16 << 20)
	waiter, err := origin.subscribeResponse(t.Context(), command.RequestID, command.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if err := origin.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, peer, []ChannelDeclaration{{Name: "main"}}, 1)
	if len(commands) != 1 || !bytes.Equal(commands[0].JSONRPC, command.JSONRPC) {
		t.Fatal("large command corrupted")
	}
	if _, err := commands[0].MarshalWireJSON(); err != nil {
		t.Fatal(err)
	}
	response := testTerminalResponse(command.RequestID)
	response.JSONResponse = sizedTunnelJSON(16 << 20)
	before := jobs.count
	if err := peer.SubmitResponse(t.Context(), "wrong", testTokenHash(), response); !errors.Is(err, ErrResponseMismatch) || jobs.count != before {
		t.Fatal("invalid binding touched storage")
	}
	for _, size := range []int{20, 3 << 20, 30} {
		notification := response
		notification.ResponseType = ResponseTypeJSONRPCNotify
		notification.JSONResponse = sizedTunnelJSON(size)
		if err := peer.SubmitResponse(t.Context(), "tunnel", testTokenHash(), notification); err != nil {
			t.Fatal(err)
		}
	}
	if err := peer.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); err != nil {
		t.Fatal(err)
	}
	sizes := []int{}
	final, err := waiter.Wait(t.Context(), func(r TunnelResponse) { sizes = append(sizes, len(r.JSONResponse)) })
	if err != nil || !bytes.Equal(final.JSONResponse, response.JSONResponse) {
		t.Fatalf("final: %v", err)
	}
	if len(sizes) != 3 || sizes[0] != 20 || sizes[1] != 3<<20 || sizes[2] != 30 {
		t.Fatalf("order: %v", sizes)
	}
	before = jobs.count
	peer.now = func() time.Time { return command.ExpiresAt.Add(time.Second) }
	if err := peer.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); err != nil || jobs.count != before {
		t.Fatalf("expired duplicate: %v", err)
	}
	if origin.responseHub.bufferedBytes != 0 {
		t.Fatal("response buffer leaked")
	}
}

func TestTunnelPayloadFailureIsNotRedelivered(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	b.cfg.MaxBodyBytes = 16 << 20
	store, objects, _ := newPayloadTestStore()
	b.payloads = store
	command := testQueuedCommand("missing-object")
	command.Scope = payloadTestScope
	command.JSONRPC = sizedTunnelJSON(3 << 20)
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	clear(objects.bodies)
	if _, err := b.Poll(t.Context(), "tunnel", testTokenHash(), []ChannelDeclaration{{Name: "main"}}, 1, time.Second); err == nil {
		t.Fatal("missing object accepted")
	}
	if commands := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1); len(commands) != 0 {
		t.Fatal("request redelivered")
	}
}

func TestTunnelReducedStorageBudget(t *testing.T) {
	servers := startTunnelNATSClusterWithStorage(t, 600<<20)
	cfg := brokerTestConfig()
	cfg.MaxBodyBytes = 16 << 20
	b, err := NewBroker(t.Context(), connectTunnelNATS(t, servers[0].ClientURL()), cfg, nil, testRequestBindings(t))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	commands, err := b.commands.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if commands.Config.MaxBytes != commandStorageBytes || commands.Config.MaxMsgSize != maxBrokerValueBytes || commands.Config.MaxMsgs != -1 || commands.Config.Replicas != 3 {
		t.Fatal("command storage contract changed")
	}
	if _, err := b.js.Stream(t.Context(), "KV_OMA_TUNNEL_REQUESTS_V1"); !errors.Is(err, jetstream.ErrStreamNotFound) {
		t.Fatalf("unexpected request KV: %v", err)
	}
	// Demonstrate this server rejects the previous budget while accepting the new one.
	old := commands.Config
	old.Name = "OLD_BUDGET"
	old.Subjects = []string{"old.budget"}
	old.MaxBytes = 4096 * (maxBrokerValueBytes + 4096)
	if _, err := b.js.CreateStream(t.Context(), old); err == nil {
		t.Fatal("old budget unexpectedly admitted")
	}
	headerSize := (&nats.Msg{Header: commandHeaders("id")}).Size()
	if headerSize <= 0 {
		t.Fatal("missing NATS header accounting")
	}
}

func TestTunnelPayloadResponseFailureStopsDelivery(t *testing.T) {
	for _, notification := range []bool{true, false} {
		t.Run(map[bool]string{true: "notification", false: "final"}[notification], func(t *testing.T) {
			cfg := brokerTestConfig()
			cfg.MaxBodyBytes = 16 << 20
			b := testNATSBroker(t, cfg)
			store, objects, _ := newPayloadTestStore()
			b.payloads = store
			command := testQueuedCommand("read-failure")
			command.Scope = payloadTestScope
			waiter, err := b.subscribeResponse(t.Context(), command.RequestID, command.ExpiresAt)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
				t.Fatal(err)
			}
			if len(pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)) != 1 {
				t.Fatal("not claimed")
			}
			response := testTerminalResponse(command.RequestID)
			response.JSONResponse = sizedTunnelJSON(3 << 20)
			if notification {
				response.ResponseType = ResponseTypeJSONRPCNotify
			}
			if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); err != nil {
				t.Fatal(err)
			}
			if notification {
				if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); err != nil {
					t.Fatal(err)
				}
			}
			clear(objects.bodies)
			seen := 0
			if _, err := waiter.Wait(t.Context(), func(TunnelResponse) { seen++ }); err == nil {
				t.Fatal("missing response object was accepted")
			}
			waiter.Close()
			if seen != 0 || b.responseHub.bufferedBytes != 0 {
				t.Fatal("failed response delivered or retained queue bytes")
			}
		})
	}
}

func TestTunnelExpiredLargeResponseCannotCompletePendingRequest(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.MaxBodyBytes = 16 << 20
	b := testNATSBroker(t, cfg)
	store, _, jobs := newPayloadTestStore()
	b.payloads = store
	command := testQueuedCommand("expired-pending")
	command.Scope = payloadTestScope
	waiter, err := b.subscribeResponse(t.Context(), command.RequestID, command.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
	b.now = func() time.Time { return command.ExpiresAt.Add(time.Second) }
	response := testTerminalResponse(command.RequestID)
	response.JSONResponse = sizedTunnelJSON(3 << 20)
	if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); !errors.Is(err, ErrResponseGone) {
		t.Fatalf("expired response=%v", err)
	}
	if jobs.count != 0 || waiter.final != nil {
		t.Fatal("expired response uploaded or delivered")
	}
}
