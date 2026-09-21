package tunnels

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

const presenceTimeout = 500 * time.Millisecond

type ConnectorSnapshot struct {
	State         string                     `json:"state"`
	InstanceCount int                        `json:"instance_count"`
	Channels      []ConnectorChannelSnapshot `json:"channels"`
}

type ConnectorChannelSnapshot struct {
	Name          string `json:"name"`
	InstanceCount int    `json:"instance_count"`
}

// ConnectorPresence is an observational store, never a delivery or authorization gate.
// Redis 8 field expiration lets each instance expire independently without cleanup jobs.
type ConnectorPresence struct {
	client *redis.Client
	ttl    time.Duration
}

func NewConnectorPresence(client *redis.Client, ttl time.Duration) *ConnectorPresence {
	if client == nil || ttl <= 0 {
		panic("tunnels: presence requires Redis and a positive TTL")
	}
	return &ConnectorPresence{client: client.WithTimeout(presenceTimeout), ttl: ttl}
}

func presenceKey(tunnelUUID string) string { return "oma:tunnel:presence:" + tunnelUUID }

func (p *ConnectorPresence) Touch(ctx context.Context, tunnelUUID, instanceID string, channels []ChannelDeclaration) error {
	ctx, cancel := context.WithTimeout(ctx, presenceTimeout)
	defer cancel()
	names := make([]string, 0, len(channels))
	for _, channel := range channels {
		names = append(names, channel.Name)
	}
	data, err := json.Marshal(names)
	if err != nil {
		return err
	}
	ttlMillis := max(int64(1), p.ttl.Milliseconds())
	return p.client.HSetEXWithArgs(ctx, presenceKey(tunnelUUID), &redis.HSetEXOptions{
		ExpirationType: redis.HSetEXExpirationPX, ExpirationVal: ttlMillis,
	}, instanceID, string(data)).Err()
}

func (p *ConnectorPresence) Snapshots(ctx context.Context, tunnelUUIDs []string) (map[string]ConnectorSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, presenceTimeout)
	defer cancel()
	pipe := p.client.Pipeline()
	commands := make(map[string]*redis.MapStringStringCmd, len(tunnelUUIDs))
	for _, id := range tunnelUUIDs {
		commands[id] = pipe.HGetAll(ctx, presenceKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	result := make(map[string]ConnectorSnapshot, len(commands))
	for id, command := range commands {
		snapshot, err := snapshotFromPresence(command.Val())
		if err != nil {
			return nil, err
		}
		result[id] = snapshot
	}
	return result, nil
}

func snapshotFromPresence(instances map[string]string) (ConnectorSnapshot, error) {
	snapshot := disconnectedSnapshot()
	counts := make(map[string]int)
	for _, value := range instances {
		var names []string
		if err := json.Unmarshal([]byte(value), &names); err != nil {
			return ConnectorSnapshot{}, fmt.Errorf("decode connector presence: %w", err)
		}
		for _, name := range names {
			counts[name]++
		}
	}
	snapshot.InstanceCount = len(instances)
	if snapshot.InstanceCount > 0 {
		snapshot.State = "connected"
	}
	for name, count := range counts {
		snapshot.Channels = append(snapshot.Channels, ConnectorChannelSnapshot{Name: name, InstanceCount: count})
	}
	sort.Slice(snapshot.Channels, func(i, j int) bool { return snapshot.Channels[i].Name < snapshot.Channels[j].Name })
	return snapshot, nil
}
