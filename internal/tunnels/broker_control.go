package tunnels

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

const maxChannelInstances = 64

type tunnelControl struct {
	TokenVersion int64                       `json:"token_version"`
	Active       bool                        `json:"active"`
	Channels     map[string]*channelControl  `json:"channels"`
	Pending      map[string]pendingAdmission `json:"pending"`
}

type channelControl struct {
	Declaration ChannelDeclaration `json:"declaration"`
	Owner       string             `json:"owner,omitempty"`
	Instances   map[string]int64   `json:"instances"`
}

type pendingAdmission struct {
	ExpiresAt int64 `json:"expires_at"`
	Bytes     int64 `json:"bytes"`
}

func newTunnelControl() tunnelControl {
	return tunnelControl{Channels: make(map[string]*channelControl), Pending: make(map[string]pendingAdmission)}
}

// A permission and suspension both conditionally update this same key. Whichever
// update commits first defines whether a delivery was granted before revocation.
// There is no expiring lock whose stale holder can grant work after suspension.
func (b *Broker) updateControl(ctx context.Context, tunnelUUID string, change func(*tunnelControl) error) error {
	key := brokerKey(tunnelUUID)
	for attempt := range brokerCASAttempts {
		state := newTunnelControl()
		stored, err := b.control.read(ctx, key, &state)
		if err != nil && !errors.Is(err, ErrRequestNotFound) {
			return err
		}
		b.pruneControl(&state)
		if err := change(&state); err != nil {
			return err
		}
		if stored.revision == 0 {
			err = b.control.create(ctx, key, state, maxControlValueBytes)
		} else {
			err = b.control.update(ctx, key, state, stored.revision, maxControlValueBytes)
		}
		if !brokerCASConflict(err) {
			return err
		}
		if err := brokerPause(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("update tunnel control: %w", ErrBrokerBusy)
}

func (b *Broker) pruneControl(state *tunnelControl) {
	now := b.now().UnixMilli()
	for id, admission := range state.Pending {
		if admission.ExpiresAt <= now {
			delete(state.Pending, id)
		}
	}
	for _, channel := range state.Channels {
		for instance, expiresAt := range channel.Instances {
			if expiresAt <= now {
				delete(channel.Instances, instance)
			}
		}
	}
}

func advanceControlToken(state *tunnelControl, version int64) error {
	if version < state.TokenVersion || (version == state.TokenVersion && !state.Active) {
		return ErrTokenRetired
	}
	if version > state.TokenVersion {
		state.TokenVersion, state.Active = version, true
		for _, channel := range state.Channels {
			clear(channel.Instances)
		}
	}
	return nil
}

func (b *Broker) RegisterConnector(ctx context.Context, tunnelUUID, instanceID string, tokenVersion int64, declarations []ChannelDeclaration) error {
	if err := validateBrokerChannels(declarations); err != nil {
		return err
	}
	return b.updateControl(ctx, tunnelUUID, func(state *tunnelControl) error {
		if err := advanceControlToken(state, tokenVersion); err != nil {
			return err
		}
		for _, declaration := range declarations {
			channel := state.Channels[declaration.Name]
			if channel == nil {
				if len(state.Channels) >= maxTunnelChannels {
					return ErrChannelLimit
				}
				channel = &channelControl{Declaration: declaration, Instances: make(map[string]int64)}
				state.Channels[declaration.Name] = channel
			}
			if channel.Declaration != declaration {
				return ErrChannelMismatch
			}
			if _, exists := channel.Instances[instanceID]; !exists && len(channel.Instances) >= maxChannelInstances {
				return ErrQueueLimit
			}
			channel.Instances[instanceID] = b.now().Add(b.cfg.PresenceTTL).UnixMilli()
		}
		return nil
	})
}

func (b *Broker) SuspendTokenVersion(ctx context.Context, tunnelUUID string, version int64) error {
	return b.updateControl(ctx, tunnelUUID, func(state *tunnelControl) error {
		if state.TokenVersion > version {
			return ErrTokenRetired
		}
		state.TokenVersion, state.Active = version, false
		for _, channel := range state.Channels {
			clear(channel.Instances)
		}
		return nil
	})
}

func (b *Broker) ActivateTokenVersion(ctx context.Context, tunnelUUID string, version int64) error {
	return b.updateControl(ctx, tunnelUUID, func(state *tunnelControl) error {
		if state.TokenVersion > version {
			return ErrTokenRetired
		}
		if version > state.TokenVersion {
			for _, channel := range state.Channels {
				clear(channel.Instances)
			}
		}
		state.TokenVersion, state.Active = version, true
		return nil
	})
}

func (b *Broker) reserveCommand(ctx context.Context, tunnelUUID string, command *queuedCommand) error {
	clientSessionID := command.Headers.Get("Mcp-Session-Id")
	return b.updateControl(ctx, tunnelUUID, func(state *tunnelControl) error {
		if !state.Active {
			return ErrNoConnector
		}
		channel := state.Channels[command.Channel]
		if channel == nil || len(channel.Instances) == 0 {
			return ErrNoConnector
		}
		if _, exists := state.Pending[command.RequestID]; exists {
			return ErrResponseMismatch
		}
		if len(state.Pending) >= b.cfg.MaxPendingRequests {
			return ErrQueueLimit
		}
		bytes := command.PayloadSize
		for _, pending := range state.Pending {
			bytes += pending.Bytes
		}
		if bytes > b.cfg.MaxPendingBytes {
			return ErrPayloadLimit
		}
		if channel.Declaration.ProcessAffinity {
			if err := b.routeAffinity(ctx, tunnelUUID, channel, command, clientSessionID); err != nil {
				return err
			}
		}
		state.Pending[command.RequestID] = pendingAdmission{ExpiresAt: command.ExpiresAt.UnixMilli(), Bytes: command.PayloadSize}
		return nil
	})
}

func (b *Broker) confirmDelivery(ctx context.Context, tunnelUUID, instanceID string, version int64, command queuedCommand) error {
	return b.updateControl(ctx, tunnelUUID, func(state *tunnelControl) error {
		if !state.Active || state.TokenVersion != version {
			return ErrTokenRetired
		}
		channel := state.Channels[command.Channel]
		if channel == nil || channel.Instances[instanceID] <= b.now().UnixMilli() {
			return ErrNoConnector
		}
		if channel.Declaration.ProcessAffinity && command.TargetInstance != instanceID {
			return ErrResponseMismatch
		}
		if !b.now().Before(command.ExpiresAt) {
			return ErrRequestExpired
		}
		return nil
	})
}

func (b *Broker) releaseCommand(ctx context.Context, tunnelUUID, requestID string) {
	// A failed release only keeps admission occupied until the original deadline.
	_ = b.updateControl(ctx, tunnelUUID, func(state *tunnelControl) error { delete(state.Pending, requestID); return nil })
}

func (b *Broker) ConnectorSnapshot(ctx context.Context, tunnelUUID string) (ConnectorSnapshot, error) {
	state := newTunnelControl()
	_, err := b.control.read(ctx, brokerKey(tunnelUUID), &state)
	if err != nil && !errors.Is(err, ErrRequestNotFound) {
		return ConnectorSnapshot{}, err
	}
	b.pruneControl(&state)
	snapshot := ConnectorSnapshot{State: "disconnected", Channels: []ConnectorChannelSnapshot{}}
	instances := make(map[string]struct{})
	if !state.Active {
		return snapshot, nil
	}
	for name, channel := range state.Channels {
		if len(channel.Instances) == 0 {
			continue
		}
		for instance := range channel.Instances {
			instances[instance] = struct{}{}
		}
		snapshot.Channels = append(snapshot.Channels, ConnectorChannelSnapshot{Name: name, ProcessAffinity: channel.Declaration.ProcessAffinity, InstanceCount: len(channel.Instances)})
	}
	sort.Slice(snapshot.Channels, func(i, j int) bool { return snapshot.Channels[i].Name < snapshot.Channels[j].Name })
	snapshot.InstanceCount = len(instances)
	if snapshot.InstanceCount > 0 {
		snapshot.State = "connected"
	}
	return snapshot, nil
}

func (b *Broker) ConnectorSnapshots(ctx context.Context, tunnelUUIDs []string) (map[string]ConnectorSnapshot, error) {
	result := make(map[string]ConnectorSnapshot, len(tunnelUUIDs))
	for _, id := range tunnelUUIDs {
		snapshot, err := b.ConnectorSnapshot(ctx, id)
		if err != nil {
			return nil, err
		}
		result[id] = snapshot
	}
	return result, nil
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
