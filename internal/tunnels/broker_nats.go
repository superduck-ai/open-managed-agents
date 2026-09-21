package tunnels

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
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
}

func NewBroker(ctx context.Context, connection *nats.Conn, cfg config.TunnelConfig) (*Broker, error) {
	return newBroker(ctx, connection, cfg, 3)
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
	// Raw JSON stays unescaped; header strings can expand sixfold in JSON.
	// Reserve metadata and transport headers before admitting any execution.
	if cfg.MaxBodyBytes > maxBrokerValueBytes-16384 || cfg.MaxHeaderBytes > (maxBrokerValueBytes-16384-cfg.MaxBodyBytes)/6 {
		return nil, fmt.Errorf("tunnel body and header limits exceed the 2 MiB broker envelope budget")
	}
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, err
	}
	commands, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: commandStreamName, Subjects: []string{commandSubjectPrefix + ">"},
		Storage: jetstream.FileStorage, Replicas: replicas, Retention: jetstream.WorkQueuePolicy,
		Discard: jetstream.DiscardNew, MaxAge: cfg.RequestTimeout,
		MaxBytes:   int64(cfg.MaxStoredRequests) * (maxBrokerValueBytes + 4096),
		MaxMsgSize: maxBrokerValueBytes, MaxConsumers: maxCommandConsumers,
		Duplicates: cfg.RequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("open tunnel command stream: %w", err)
	}
	requests, err := openBrokerStore(ctx, js, requestBucketName, int64(cfg.MaxStoredRequests), maxBrokerValueBytes, brokerRequestRetention(cfg.RequestTimeout, cfg.TombstoneTTL), replicas)
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

type requestRecord struct {
	TunnelID    string            `json:"tunnel_id"`
	TokenHash   [sha256.Size]byte `json:"token_hash"`
	TunnelUUID  string            `json:"tunnel_uuid"`
	RequestID   string            `json:"request_id"`
	Channel     string            `json:"channel"`
	CommandType CommandType       `json:"command_type"`
	ExpiresAt   time.Time         `json:"expires_at"`
	State       string            `json:"state"`
	Origin      string            `json:"origin"`
	Response    *TunnelResponse   `json:"response,omitempty"`
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
	state := requestRecord{TunnelUUID: tunnelUUID, TunnelID: tunnelID, RequestID: command.RequestID, Channel: command.Channel,
		CommandType: command.CommandType, ExpiresAt: command.ExpiresAt, State: "queued", Origin: b.responseHub.subject}
	key := brokerKey(command.RequestID)
	if err := b.requests.create(ctx, key, state, maxBrokerValueBytes); err != nil {
		// An uncertain create leaves at most an orphan; no command was published.
		return err
	}
	data, err := encodeTunnelJSON(command, maxBrokerValueBytes)
	if err == nil {
		_, err = b.js.Publish(ctx, commandSubject(tunnelUUID, command.Channel), data, jetstream.WithMsgID(key))
	}
	if err != nil {
		// Cancellation races dispatch by revision; never overwrite completion.
		_ = b.Cancel(ctx, tunnelUUID, command.RequestID)
		return brokerCapacityError(err)
	}
	return nil
}

func commandSubject(tunnelUUID, channel string) string {
	return commandSubjectPrefix + brokerKey(tunnelUUID) + "." + channel + ".shared"
}

func (b *Broker) readRequestByID(ctx context.Context, requestID string) (requestRecord, uint64, error) {
	var record requestRecord
	stored, err := b.requests.read(ctx, brokerKey(requestID), &record)
	if err != nil {
		return record, 0, err
	}
	if record.RequestID != requestID {
		return record, 0, ErrResponseMismatch
	}
	return record, stored.revision, nil
}

func (b *Broker) readRequest(ctx context.Context, tunnelUUID, requestID string) (requestRecord, uint64, error) {
	record, revision, err := b.readRequestByID(ctx, requestID)
	if err != nil {
		return record, 0, err
	}
	if record.TunnelUUID != tunnelUUID {
		return record, 0, ErrResponseMismatch
	}
	return record, revision, nil
}

func (b *Broker) GetResponse(ctx context.Context, tunnelUUID, requestID string) (*TunnelResponse, string, error) {
	record, _, err := b.readRequest(ctx, tunnelUUID, requestID)
	if err != nil {
		return nil, "", err
	}
	if record.State == "completed" {
		return record.Response, record.State, nil
	}
	if !b.now().Before(record.ExpiresAt) {
		return nil, "expired", nil
	}
	return nil, record.State, nil
}

func (b *Broker) Cancel(ctx context.Context, tunnelUUID, requestID string) error {
	for attempt := range brokerCASAttempts {
		record, revision, err := b.readRequest(ctx, tunnelUUID, requestID)
		if err != nil {
			return err
		}
		if record.State == "completed" || record.State == "canceled" {
			return nil
		}
		record.State = "canceled"
		err = b.requests.update(ctx, brokerKey(requestID), record, revision, maxBrokerValueBytes)
		if brokerCASConflict(err) {
			if err := brokerPause(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		publishBrokerSignal(b.connection, record.Origin, brokerKey(record.TunnelUUID, record.RequestID))
		return nil
	}
	return ErrBrokerBusy
}

func (b *Broker) SubmitResponse(ctx context.Context, tunnelID string, tokenHash [sha256.Size]byte, response TunnelResponse) error {
	for attempt := range brokerCASAttempts {
		record, revision, err := b.readRequestByID(ctx, response.RequestID)
		if err != nil {
			return err
		}
		if err := validateResponseBinding(record, tunnelID, tokenHash, response); err != nil {
			return err
		}
		if record.State == "completed" {
			return nil
		}
		if record.State == "canceled" {
			return ErrRequestCanceled
		}
		if !b.now().Before(record.ExpiresAt) {
			return ErrRequestExpired
		}
		if !response.terminal() {
			return b.responseHub.forward(ctx, record.Origin, record.TunnelUUID, response)
		}
		record.State, record.Response = "completed", &response
		err = b.requests.update(ctx, brokerKey(response.RequestID), record, revision, maxBrokerValueBytes)
		if brokerCASConflict(err) {
			if err := brokerPause(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		publishBrokerSignal(b.connection, record.Origin, brokerKey(record.TunnelUUID, record.RequestID))
		return nil
	}
	return ErrBrokerBusy
}

func validateResponseBinding(record requestRecord, tunnelID string, tokenHash [sha256.Size]byte, response TunnelResponse) error {
	if record.TunnelID != tunnelID || subtle.ConstantTimeCompare(record.TokenHash[:], tokenHash[:]) != 1 || record.Channel != response.Channel || record.State == "queued" {
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

func brokerWaitableError(err error) bool {
	var apiError *jetstream.APIError
	if errors.As(err, &apiError) && apiError.Code == 503 {
		return true
	}
	return errors.Is(err, ErrRequestNotFound) || errors.Is(err, nats.ErrTimeout) || errors.Is(err, nats.ErrDisconnected) || errors.Is(err, nats.ErrReconnectBufExceeded) || errors.Is(err, nats.ErrNoResponders) || errors.Is(err, context.DeadlineExceeded)
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
