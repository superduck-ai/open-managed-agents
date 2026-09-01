package tunnels

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/redis/go-redis/v9"
)

var (
	ErrNoConnector      = errors.New("tunnels: no live connector")
	ErrQueueLimit       = errors.New("tunnels: pending request limit exceeded")
	ErrPayloadLimit     = errors.New("tunnels: pending payload limit exceeded")
	ErrChannelLimit     = errors.New("tunnels: channel limit exceeded")
	ErrChannelInvalid   = errors.New("tunnels: channel is invalid")
	ErrChannelMismatch  = errors.New("tunnels: channel declaration mismatch")
	ErrRequestNotFound  = errors.New("tunnels: request not found")
	ErrResponseMismatch = errors.New("tunnels: response binding mismatch")
	ErrRequestExpired   = errors.New("tunnels: request expired")
	ErrRequestCanceled  = errors.New("tunnels: request canceled")
	ErrTokenRetired     = errors.New("tunnels: token version is no longer active")
)

const (
	maxTunnelChannels            = 32
	brokerCleanupBatchSize       = 512
	durableTerminalDeliveryGrace = 100 * time.Millisecond
)

type Broker struct {
	client      *redis.Client
	cfg         config.TunnelConfig
	now         func() time.Time
	responseHub *responseHub
}

type ConnectorSnapshot struct {
	State         string                     `json:"state"`
	InstanceCount int                        `json:"instance_count"`
	Channels      []ConnectorChannelSnapshot `json:"channels"`
}

type ConnectorChannelSnapshot struct {
	Name            string `json:"name"`
	ProcessAffinity bool   `json:"process_affinity"`
	InstanceCount   int    `json:"instance_count"`
}

type connectorSnapshotCommands struct {
	channels *redis.StringSliceCmd
	metadata *redis.MapStringStringCmd
}

type connectorPresenceCommand struct {
	tunnelUUID string
	channel    string
	members    *redis.StringSliceCmd
}

type responseWaiter struct {
	broker       *Broker
	subscription *responseSubscription
	tunnelUUID   string
	requestID    string
	preEnqueue   bool
}

func NewBroker(client *redis.Client, cfg config.TunnelConfig) *Broker {
	if client == nil {
		panic("tunnels: redis client is required")
	}
	return &Broker{
		client: client, cfg: cfg, now: func() time.Time { return time.Now().UTC() },
		responseHub: newResponseHub(client),
	}
}

func (b *Broker) Ping(ctx context.Context) error {
	return b.client.Ping(ctx).Err()
}

func (b *Broker) ConnectorSnapshot(ctx context.Context, tunnelUUID string) (ConnectorSnapshot, error) {
	snapshots, err := b.ConnectorSnapshots(ctx, []string{tunnelUUID})
	if err != nil {
		return ConnectorSnapshot{}, err
	}
	return snapshots[tunnelUUID], nil
}

func (b *Broker) ConnectorSnapshots(ctx context.Context, tunnelUUIDs []string) (map[string]ConnectorSnapshot, error) {
	snapshots := make(map[string]ConnectorSnapshot, len(tunnelUUIDs))
	if len(tunnelUUIDs) == 0 {
		return snapshots, nil
	}
	metadataPipeline := b.client.Pipeline()
	commands := make(map[string]connectorSnapshotCommands, len(tunnelUUIDs))
	for _, tunnelUUID := range tunnelUUIDs {
		commands[tunnelUUID] = connectorSnapshotCommands{
			channels: metadataPipeline.SMembers(ctx, b.channelsKey(tunnelUUID)),
			metadata: metadataPipeline.HGetAll(ctx, b.channelMetadataKey(tunnelUUID)),
		}
	}
	if _, err := metadataPipeline.Exec(ctx); err != nil {
		return nil, fmt.Errorf("read tunnel connector metadata: %w", err)
	}

	now := strconv.FormatInt(b.now().UnixMilli(), 10)
	presencePipeline := b.client.Pipeline()
	presenceCommands := make([]connectorPresenceCommand, 0)
	channelMetadata := make(map[string]map[string]string, len(tunnelUUIDs))
	for _, tunnelUUID := range tunnelUUIDs {
		channelNames, err := commands[tunnelUUID].channels.Result()
		if err != nil {
			return nil, fmt.Errorf("read tunnel connector channels: %w", err)
		}
		metadata, err := commands[tunnelUUID].metadata.Result()
		if err != nil {
			return nil, fmt.Errorf("read tunnel connector channel metadata: %w", err)
		}
		sort.Strings(channelNames)
		channelMetadata[tunnelUUID] = metadata
		for _, channel := range channelNames {
			key := b.presenceKey(tunnelUUID, channel)
			presencePipeline.ZRemRangeByScore(ctx, key, "-inf", now)
			presenceCommands = append(presenceCommands, connectorPresenceCommand{
				tunnelUUID: tunnelUUID,
				channel:    channel,
				members:    presencePipeline.ZRange(ctx, key, 0, -1),
			})
		}
	}
	if len(presenceCommands) > 0 {
		if _, err := presencePipeline.Exec(ctx); err != nil {
			return nil, fmt.Errorf("read tunnel connector presence: %w", err)
		}
	}

	instances := make(map[string]map[string]struct{}, len(tunnelUUIDs))
	for _, tunnelUUID := range tunnelUUIDs {
		snapshots[tunnelUUID] = ConnectorSnapshot{State: "disconnected", Channels: []ConnectorChannelSnapshot{}}
		instances[tunnelUUID] = make(map[string]struct{})
	}
	for _, command := range presenceCommands {
		members, err := command.members.Result()
		if err != nil {
			return nil, fmt.Errorf("read tunnel connector instances: %w", err)
		}
		if len(members) == 0 {
			continue
		}
		for _, instanceID := range members {
			instances[command.tunnelUUID][instanceID] = struct{}{}
		}
		processAffinity, _ := strconv.ParseBool(channelMetadata[command.tunnelUUID][command.channel])
		snapshot := snapshots[command.tunnelUUID]
		snapshot.Channels = append(snapshot.Channels, ConnectorChannelSnapshot{
			Name: command.channel, ProcessAffinity: processAffinity, InstanceCount: len(members),
		})
		snapshots[command.tunnelUUID] = snapshot
	}
	for _, tunnelUUID := range tunnelUUIDs {
		snapshot := snapshots[tunnelUUID]
		snapshot.InstanceCount = len(instances[tunnelUUID])
		if snapshot.InstanceCount > 0 {
			snapshot.State = "connected"
		}
		snapshots[tunnelUUID] = snapshot
	}
	return snapshots, nil
}

func (b *Broker) RegisterConnector(ctx context.Context, tunnelUUID, instanceID string, tokenVersion int64, channels []ChannelDeclaration) error {
	if err := validateBrokerChannels(channels); err != nil {
		return err
	}
	keys := []string{b.channelsKey(tunnelUUID), b.channelMetadataKey(tunnelUUID), b.activeTokenVersionKey(tunnelUUID)}
	args := []any{instanceID, b.now().Add(b.cfg.PresenceTTL).UnixMilli(), b.cfg.PresenceTTL.Milliseconds(), len(channels)}
	for _, channel := range channels {
		keys = append(keys, b.presenceKey(tunnelUUID, channel.Name))
		args = append(args, channel.Name, strconv.FormatBool(channel.ProcessAffinity))
	}
	args = append(args, tokenVersion, b.keyPrefix(tunnelUUID)+"presence:")
	result, err := registerConnectorScript.Run(ctx, b.client, keys, args...).Int64()
	if err != nil {
		return fmt.Errorf("register tunnel connector: %w", err)
	}
	switch result {
	case 1:
		return nil
	case -1:
		return ErrChannelLimit
	case -2:
		return ErrChannelMismatch
	case -3:
		return ErrTokenRetired
	default:
		return fmt.Errorf("register tunnel connector: unexpected result %d", result)
	}
}

func (b *Broker) Enqueue(ctx context.Context, tunnelUUID string, command queuedCommand) error {
	if command.Channel == "" {
		command.Channel = "main"
	}
	if !channelNamePattern.MatchString(command.Channel) {
		return ErrChannelInvalid
	}
	if err := b.cleanup(ctx, tunnelUUID); err != nil {
		return err
	}
	command.ExpiresAtMS = command.ExpiresAt.UnixMilli()
	state := brokerRequestState{queuedCommand: command, State: "queued"}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode tunnel request state: %w", err)
	}
	now := b.now()
	result, err := enqueueScript.Run(ctx, b.client, []string{
		b.requestsKey(tunnelUUID),
		b.queueKey(tunnelUUID, command.Channel),
		b.presenceKey(tunnelUUID, command.Channel),
		b.budgetKey(tunnelUUID),
		b.expiryKey(tunnelUUID),
	}, command.RequestID, string(encoded), now.UnixMilli(), command.ExpiresAt.UnixMilli(), command.PayloadSize,
		b.cfg.MaxPendingRequests, b.cfg.MaxPendingBytes, b.wakeChannel(tunnelUUID)).Int64()
	if err != nil {
		return fmt.Errorf("enqueue tunnel request: %w", err)
	}
	switch result {
	case 1:
		return nil
	case -1:
		return ErrNoConnector
	case -2:
		return ErrQueueLimit
	case -3:
		return ErrPayloadLimit
	default:
		return fmt.Errorf("enqueue tunnel request: unexpected result %d", result)
	}
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

func (b *Broker) Claim(ctx context.Context, tunnelUUID, instanceID string, tokenVersion int64, channels []ChannelDeclaration, limit int) ([]ClaimedCommand, error) {
	if err := validateBrokerChannels(channels); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return []ClaimedCommand{}, nil
	}
	if err := b.cleanup(ctx, tunnelUUID); err != nil {
		return nil, err
	}
	if err := b.ensureActiveTokenVersion(ctx, tunnelUUID, tokenVersion); err != nil {
		return nil, err
	}
	if err := b.RegisterConnector(ctx, tunnelUUID, instanceID, tokenVersion, channels); err != nil {
		return nil, err
	}
	keys := []string{
		b.requestsKey(tunnelUUID), b.expiryKey(tunnelUUID), b.budgetKey(tunnelUUID),
		b.affinityKey(tunnelUUID), b.affinityExpiryKey(tunnelUUID),
		b.activeTokenVersionKey(tunnelUUID),
	}
	for _, channel := range channels {
		keys = append(keys, b.queueKey(tunnelUUID, channel.Name))
	}
	args := []any{
		b.keyPrefix(tunnelUUID), instanceID, tokenVersion, limit, b.now().UnixMilli(),
		b.cfg.TombstoneTTL.Milliseconds(), b.cfg.PresenceTTL.Milliseconds(), len(channels), b.cfg.MaxPendingRequests,
	}
	for _, channel := range channels {
		args = append(args, strconv.FormatBool(channel.ProcessAffinity))
	}
	for range limit {
		shardToken, err := randomOpaqueToken(24)
		if err != nil {
			return nil, fmt.Errorf("generate shard token: %w", err)
		}
		args = append(args, shardToken)
	}
	values, err := claimScript.Run(ctx, b.client, keys, args...).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("claim tunnel requests: %w", err)
	}
	if len(values) == 1 && values[0] == "__token_retired__" {
		return nil, ErrTokenRetired
	}
	commands := make([]ClaimedCommand, 0, len(values))
	now := b.now()
	for _, value := range values {
		var state brokerRequestState
		if err := json.Unmarshal([]byte(value), &state); err != nil {
			return nil, fmt.Errorf("decode claimed tunnel request: %w", err)
		}
		remaining := state.ExpiresAt.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		commands = append(commands, ClaimedCommand{
			RequestID: state.RequestID, ShardToken: state.ShardToken,
			CommandType: state.CommandType, Channel: state.Channel, CreatedAt: state.CreatedAt,
			Headers: state.Headers, JSONRPC: state.JSONRPC, ResponseTimeout: remaining,
		})
	}
	return commands, nil
}

func (b *Broker) ensureActiveTokenVersion(ctx context.Context, tunnelUUID string, tokenVersion int64) error {
	result, err := ensureActiveTokenVersionScript.Run(
		ctx,
		b.client,
		[]string{b.activeTokenVersionKey(tunnelUUID)},
		tokenVersion,
	).Int64()
	if err != nil {
		return fmt.Errorf("ensure active tunnel token version: %w", err)
	}
	if result != 1 {
		return ErrTokenRetired
	}
	return nil
}

func (b *Broker) SuspendTokenVersion(ctx context.Context, tunnelUUID string, tokenVersion int64) error {
	result, err := suspendActiveTokenVersionScript.Run(
		ctx,
		b.client,
		[]string{b.activeTokenVersionKey(tunnelUUID), b.channelsKey(tunnelUUID)},
		tokenVersion,
		b.wakeChannel(tunnelUUID),
		b.keyPrefix(tunnelUUID),
	).Int64()
	if err != nil {
		return fmt.Errorf("suspend tunnel token version: %w", err)
	}
	if result != 1 {
		return ErrTokenRetired
	}
	return nil
}

func (b *Broker) ActivateTokenVersion(ctx context.Context, tunnelUUID string, tokenVersion int64) error {
	result, err := activateTokenVersionScript.Run(
		ctx,
		b.client,
		[]string{b.activeTokenVersionKey(tunnelUUID)},
		tokenVersion,
		b.wakeChannel(tunnelUUID),
	).Int64()
	if err != nil {
		return fmt.Errorf("activate tunnel token version: %w", err)
	}
	if result != 1 {
		return ErrTokenRetired
	}
	return nil
}

func (b *Broker) Poll(ctx context.Context, tunnelUUID, instanceID string, tokenVersion int64, channels []ChannelDeclaration, limit int, timeout time.Duration) ([]ClaimedCommand, error) {
	pubsub := b.client.Subscribe(ctx, b.wakeChannel(tunnelUUID))
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		return nil, fmt.Errorf("subscribe tunnel poll wakeups: %w", err)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		commands, err := b.Claim(ctx, tunnelUUID, instanceID, tokenVersion, channels, limit)
		if err != nil || len(commands) > 0 {
			return commands, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return []ClaimedCommand{}, nil
		case _, open := <-pubsub.Channel():
			if !open {
				return nil, errors.New("tunnel poll subscription closed")
			}
		}
	}
}

func (b *Broker) SubmitResponse(ctx context.Context, tunnelUUID, instanceID string, tokenVersion int64, shardToken string, response TunnelResponse) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode tunnel response: %w", err)
	}
	terminal := 0
	if response.terminal() {
		terminal = 1
	}
	result, err := submitResponseScript.Run(ctx, b.client, []string{
		b.requestsKey(tunnelUUID), b.budgetKey(tunnelUUID), b.expiryKey(tunnelUUID),
		b.affinityKey(tunnelUUID), b.affinityExpiryKey(tunnelUUID),
		b.channelMetadataKey(tunnelUUID),
	}, response.RequestID, instanceID, tokenVersion, shardToken, string(encoded), terminal,
		b.now().UnixMilli(), b.cfg.TombstoneTTL.Milliseconds(), b.responseChannel(tunnelUUID, response.RequestID),
		b.cfg.PresenceTTL.Milliseconds(), response.ResponseHeaders.Get("Mcp-Session-Id")).Int64()
	if err != nil {
		return fmt.Errorf("submit tunnel response: %w", err)
	}
	switch result {
	case 1, 2:
		return nil
	case -1, -3:
		return ErrRequestNotFound
	case -2:
		return ErrResponseMismatch
	case -4:
		return ErrRequestExpired
	default:
		return fmt.Errorf("submit tunnel response: unexpected result %d", result)
	}
}

func (b *Broker) GetResponse(ctx context.Context, tunnelUUID, requestID string) (*TunnelResponse, string, error) {
	if err := b.cleanup(ctx, tunnelUUID); err != nil {
		return nil, "", err
	}
	value, err := b.client.HGet(ctx, b.requestsKey(tunnelUUID), requestID).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, "", ErrRequestNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("read tunnel request state: %w", err)
	}
	var state brokerRequestState
	if err := json.Unmarshal(value, &state); err != nil {
		return nil, "", fmt.Errorf("decode tunnel request state: %w", err)
	}
	return state.Response, state.State, nil
}

func (b *Broker) WaitResponse(ctx context.Context, tunnelUUID, requestID string, onNotification func(TunnelResponse)) (TunnelResponse, error) {
	waiter, err := b.subscribeResponse(ctx, tunnelUUID, requestID, false)
	if err != nil {
		return TunnelResponse{}, err
	}
	defer waiter.Close()
	return waiter.Wait(ctx, onNotification)
}

func (b *Broker) subscribeResponse(ctx context.Context, tunnelUUID, requestID string, preEnqueue bool) (*responseWaiter, error) {
	subscription, err := b.responseHub.subscribe(ctx, b.responseChannel(tunnelUUID, requestID))
	if err != nil {
		return nil, err
	}
	return &responseWaiter{
		broker: b, subscription: subscription, tunnelUUID: tunnelUUID, requestID: requestID,
		preEnqueue: preEnqueue,
	}, nil
}

func (w *responseWaiter) Close() {
	if w != nil && w.subscription != nil {
		w.subscription.close()
	}
}

func (w *responseWaiter) Wait(ctx context.Context, onNotification func(TunnelResponse)) (TunnelResponse, error) {
	for {
		response, state, err := w.broker.GetResponse(ctx, w.tunnelUUID, w.requestID)
		if err != nil {
			return TunnelResponse{}, err
		}
		switch state {
		case "completed":
			if response == nil {
				return TunnelResponse{}, errors.New("completed tunnel request has no response")
			}
			if !w.preEnqueue {
				w.broker.acknowledgeResponse(ctx, w.tunnelUUID, w.requestID)
				return *response, nil
			}
			payload, received := w.receiveBeforeDurableFallback(ctx)
			if !received {
				w.broker.acknowledgeResponse(ctx, w.tunnelUUID, w.requestID)
				return *response, nil
			}
			if terminalResponse, terminal := consumeResponseMessage(payload, onNotification); terminal {
				w.broker.acknowledgeResponse(ctx, w.tunnelUUID, w.requestID)
				return terminalResponse, nil
			}
			continue
		case "expired":
			return TunnelResponse{}, ErrRequestExpired
		case "canceled":
			return TunnelResponse{}, ErrRequestCanceled
		}
		payload, err := w.subscription.receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return TunnelResponse{}, err
			}
			if response, terminal, stateErr := w.durableTerminal(ctx); stateErr != nil {
				return TunnelResponse{}, stateErr
			} else if terminal {
				return response, nil
			}
			if reconnectErr := w.reconnect(ctx); reconnectErr != nil {
				if response, terminal, stateErr := w.durableTerminal(ctx); stateErr != nil {
					return TunnelResponse{}, stateErr
				} else if terminal {
					return response, nil
				}
				return TunnelResponse{}, fmt.Errorf("receive tunnel response: %v; reconnect subscription: %w", err, reconnectErr)
			}
			if response, terminal, stateErr := w.durableTerminal(ctx); stateErr != nil {
				return TunnelResponse{}, stateErr
			} else if terminal {
				return response, nil
			}
			continue
		}
		if response, terminal := consumeResponseMessage(payload, onNotification); terminal {
			w.broker.acknowledgeResponse(ctx, w.tunnelUUID, w.requestID)
			return response, nil
		}
	}
}

func (w *responseWaiter) receiveBeforeDurableFallback(ctx context.Context) (string, bool) {
	deliveryCtx, cancel := context.WithTimeout(ctx, durableTerminalDeliveryGrace)
	defer cancel()
	payload, err := w.subscription.receive(deliveryCtx)
	if err != nil {
		return "", false
	}
	return payload, true
}

func (w *responseWaiter) durableTerminal(ctx context.Context) (TunnelResponse, bool, error) {
	response, state, err := w.broker.GetResponse(ctx, w.tunnelUUID, w.requestID)
	if err != nil {
		return TunnelResponse{}, false, err
	}
	switch state {
	case "completed":
		if response == nil {
			return TunnelResponse{}, false, errors.New("completed tunnel request has no response")
		}
		w.broker.acknowledgeResponse(ctx, w.tunnelUUID, w.requestID)
		return *response, true, nil
	case "expired":
		return TunnelResponse{}, false, ErrRequestExpired
	case "canceled":
		return TunnelResponse{}, false, ErrRequestCanceled
	default:
		return TunnelResponse{}, false, nil
	}
}

func (w *responseWaiter) reconnect(ctx context.Context) error {
	if w.subscription != nil {
		w.subscription.close()
	}
	subscription, err := w.broker.responseHub.subscribe(ctx, w.broker.responseChannel(w.tunnelUUID, w.requestID))
	if err != nil {
		return err
	}
	w.subscription = subscription
	return nil
}

func (b *Broker) acknowledgeResponse(ctx context.Context, tunnelUUID, requestID string) {
	// Delivery has already succeeded, so a transient Redis cleanup failure must
	// not turn a valid MCP response into an error. The tombstone TTL remains the
	// bounded fallback cleanup path.
	_, _ = acknowledgeResponseScript.Run(
		ctx,
		b.client,
		[]string{b.requestsKey(tunnelUUID)},
		requestID,
	).Result()
}

func consumeResponseMessage(payload string, onNotification func(TunnelResponse)) (TunnelResponse, bool) {
	var response TunnelResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil || response.RequestID == "" {
		return TunnelResponse{}, false
	}
	if response.terminal() {
		return response, true
	}
	if onNotification != nil {
		onNotification(response)
	}
	return TunnelResponse{}, false
}

func (b *Broker) Cancel(ctx context.Context, tunnelUUID, requestID string) error {
	result, err := cancelRequestScript.Run(ctx, b.client, []string{
		b.requestsKey(tunnelUUID), b.budgetKey(tunnelUUID), b.expiryKey(tunnelUUID),
	}, requestID, b.keyPrefix(tunnelUUID), b.now().UnixMilli(), b.cfg.TombstoneTTL.Milliseconds(), b.responseChannel(tunnelUUID, requestID)).Int64()
	if err != nil {
		return fmt.Errorf("cancel tunnel request: %w", err)
	}
	if result == -1 {
		return ErrRequestNotFound
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

func (b *Broker) cleanup(ctx context.Context, tunnelUUID string) error {
	for {
		processed, err := cleanupRequestsScript.Run(ctx, b.client, []string{
			b.requestsKey(tunnelUUID), b.expiryKey(tunnelUUID), b.budgetKey(tunnelUUID),
			b.affinityKey(tunnelUUID), b.affinityExpiryKey(tunnelUUID),
		}, b.now().UnixMilli(), b.cfg.TombstoneTTL.Milliseconds(), b.keyPrefix(tunnelUUID), brokerCleanupBatchSize).Int64()
		if err != nil {
			return fmt.Errorf("cleanup tunnel requests: %w", err)
		}
		if processed < brokerCleanupBatchSize {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (b *Broker) keyPrefix(tunnelUUID string) string {
	return "oma:tunnel:{" + tunnelUUID + "}:"
}

func (b *Broker) requestsKey(tunnelUUID string) string { return b.keyPrefix(tunnelUUID) + "requests" }
func (b *Broker) budgetKey(tunnelUUID string) string   { return b.keyPrefix(tunnelUUID) + "budget" }
func (b *Broker) expiryKey(tunnelUUID string) string   { return b.keyPrefix(tunnelUUID) + "expiry" }
func (b *Broker) channelsKey(tunnelUUID string) string { return b.keyPrefix(tunnelUUID) + "channels" }
func (b *Broker) activeTokenVersionKey(tunnelUUID string) string {
	return b.keyPrefix(tunnelUUID) + "active_token_version"
}
func (b *Broker) affinityKey(tunnelUUID string) string { return b.keyPrefix(tunnelUUID) + "affinity" }
func (b *Broker) affinityExpiryKey(tunnelUUID string) string {
	return b.keyPrefix(tunnelUUID) + "affinity_expiry"
}
func (b *Broker) channelMetadataKey(tunnelUUID string) string {
	return b.keyPrefix(tunnelUUID) + "channel_meta"
}
func (b *Broker) presenceKey(tunnelUUID, channel string) string {
	return b.keyPrefix(tunnelUUID) + "presence:" + channel
}
func (b *Broker) queueKey(tunnelUUID, channel string) string {
	return b.keyPrefix(tunnelUUID) + "queue:" + channel
}
func (b *Broker) wakeChannel(tunnelUUID string) string { return b.keyPrefix(tunnelUUID) + "wake" }
func (b *Broker) responseChannel(tunnelUUID, requestID string) string {
	return b.keyPrefix(tunnelUUID) + "response:" + requestID
}
