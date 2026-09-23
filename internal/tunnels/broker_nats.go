package tunnels

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const maxTunnelChannels = 32

type Broker struct {
	connection  *nats.Conn
	js          jetstream.JetStream
	commands    jetstream.Stream
	requests    *brokerStore
	cfg         config.TunnelConfig
	now         func() time.Time
	responseHub *responseHub
	payloads    *PayloadStore
	pollCursor  atomic.Uint64
}

func NewBroker(ctx context.Context, connection *nats.Conn, cfg config.TunnelConfig, payloads *PayloadStore) (*Broker, error) {
	b, err := newBroker(ctx, connection, cfg, 3)
	if err == nil {
		b.payloads = payloads
	}
	return b, err
}

func newBroker(ctx context.Context, connection *nats.Conn, cfg config.TunnelConfig, replicas int) (*Broker, error) {
	if connection == nil || !connection.IsConnected() {
		return nil, nats.ErrDisconnected
	}
	if connection.MaxPayload() < maxBrokerValueBytes {
		return nil, fmt.Errorf("tunnel NATS max_payload must be at least %d bytes", maxBrokerValueBytes)
	}
	if cfg.MaxStoredRequests <= 0 || cfg.RequestTimeout <= 0 || cfg.TombstoneTTL <= 0 {
		return nil, fmt.Errorf("invalid tunnel broker capacity or retention")
	}
	// Body limits are independent of NATS: oversized bodies use object references.
	if cfg.MaxBodyBytes <= 0 || cfg.MaxHeaderBytes <= 0 || cfg.MaxHeaderBytes > (maxBrokerValueBytes-16384)/6 {
		return nil, fmt.Errorf("invalid tunnel body or header limits")
	}
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, err
	}
	commands, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: commandStreamName, Subjects: []string{commandSubjectPrefix + ">"},
		Storage: jetstream.FileStorage, Replicas: replicas, Retention: jetstream.WorkQueuePolicy,
		Discard: jetstream.DiscardNew, MaxAge: cfg.RequestTimeout, MaxMsgs: int64(cfg.MaxStoredRequests),
		MaxBytes:   int64(cfg.MaxStoredRequests) * (maxBrokerValueBytes + 4096),
		MaxMsgSize: maxBrokerValueBytes, MaxConsumers: maxCommandConsumers,
		Duplicates: cfg.RequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("open tunnel command stream: %w", err)
	}
	requests, err := openBrokerStore(ctx, js, requestBucketName, int64(cfg.MaxStoredRequests), maxRequestBindingBytes, brokerRequestRetention(cfg.RequestTimeout, cfg.TombstoneTTL), replicas)
	if err != nil {
		return nil, err
	}
	hub, err := newResponseHub(ctx, connection, cfg.MaxStoredRequests)
	if err != nil {
		return nil, err
	}
	return &Broker{connection: connection, js: js, commands: commands, requests: requests, cfg: cfg, now: time.Now, responseHub: hub}, nil
}

func (b *Broker) Close() { b.responseHub.close() }

func (b *Broker) Ping(ctx context.Context) error {
	if !b.connection.IsConnected() {
		return nats.ErrDisconnected
	}
	if _, err := b.commands.Info(ctx); err != nil {
		return err
	}
	_, err := b.requests.stream.Info(ctx)
	return err
}

// requestRecord is an immutable response binding, created only before delivery.
// No execution state or response body is persisted.
type requestRecord struct {
	Scope       payloadScope      `json:"scope"`
	TunnelID    string            `json:"tunnel_id"`
	TokenHash   [sha256.Size]byte `json:"token_hash"`
	Channel     string            `json:"channel"`
	CommandType CommandType       `json:"command_type"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Origin      string            `json:"origin"`
}

func (b *Broker) Enqueue(ctx context.Context, tunnelUUID, tunnelID string, command queuedCommand) error {
	if command.Channel == "" {
		command.Channel = "main"
	}
	if !channelNamePattern.MatchString(command.Channel) {
		return ErrChannelInvalid
	}
	if !b.now().Before(command.ExpiresAt) || command.ExpiresAt.After(b.now().Add(b.cfg.RequestTimeout)) {
		return ErrRequestExpired
	}
	command.TunnelID, command.Origin = tunnelID, b.responseHub.subject
	data, err := b.encodeCommand(ctx, command)
	if err != nil {
		return err
	}
	_, err = b.js.PublishMsg(ctx, &nats.Msg{Subject: commandSubject(tunnelUUID, command.Channel), Header: commandHeaders(command.RequestID), Data: data})
	return brokerCapacityError(err)
}

func commandSubject(tunnelUUID, channel string) string {
	return commandSubjectPrefix + brokerKey(tunnelUUID) + "." + channel + ".shared"
}

func (b *Broker) readRequestByID(ctx context.Context, requestID string) (requestRecord, error) {
	var record requestRecord
	err := b.requests.read(ctx, brokerKey(requestID), &record)
	return record, err
}

func (b *Broker) SubmitResponse(ctx context.Context, tunnelID string, tokenHash [sha256.Size]byte, response TunnelResponse) error {
	record, err := b.readRequestByID(ctx, response.RequestID)
	if err != nil {
		return err
	}
	if err := validateResponseBinding(record, tunnelID, tokenHash, response); err != nil {
		return err
	}
	// The origin alone knows whether an expired request already completed. It
	// checks its receipt before the deadline, allowing only completed duplicates.
	envelope, err := b.encodeResponse(ctx, record, response)
	if err != nil {
		return err
	}
	return b.responseHub.forwardData(ctx, record.Origin, envelope)
}

func validateResponseBinding(record requestRecord, tunnelID string, tokenHash [sha256.Size]byte, response TunnelResponse) error {
	if record.TunnelID != tunnelID || subtle.ConstantTimeCompare(record.TokenHash[:], tokenHash[:]) != 1 || record.Channel != response.Channel {
		return ErrResponseMismatch
	}
	valid := false
	switch record.CommandType {
	case CommandTypeJSONRPC:
		valid = response.ResponseType == ResponseTypeJSONRPC || response.ResponseType == ResponseTypeJSONRPCNotify || response.ResponseType == ResponseTypeNotifyAck
	case CommandTypeOAuthDiscovery:
		valid = response.ResponseType == ResponseTypeOAuth
	case CommandTypeSessionTermination:
		valid = response.ResponseType == ResponseTypeSessionTermination
	}
	if !valid {
		return ErrResponseMismatch
	}
	return nil
}

func randomOpaqueToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	clear(value)
	return token, nil
}

func validateBrokerChannels(channels []ChannelDeclaration) error {
	if len(channels) == 0 || len(channels) > maxTunnelChannels {
		return ErrChannelLimit
	}
	seen := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		if !channelNamePattern.MatchString(channel.Name) {
			return ErrChannelInvalid
		}
		if _, duplicate := seen[channel.Name]; duplicate {
			return ErrChannelInvalid
		}
		seen[channel.Name] = struct{}{}
	}
	return nil
}

func brokerRequestRetention(requestTimeout, tombstoneTTL time.Duration) time.Duration {
	return requestTimeout + tombstoneTTL
}
