package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"
)

// Affinity sessions occupy the same bounded KV slots as requests. Reserving the
// slot before initialize executes guarantees room for its downstream session ID.
type affinitySession struct {
	TunnelUUID string    `json:"tunnel_uuid"`
	ID         string    `json:"id"`
	Channel    string    `json:"channel"`
	InstanceID string    `json:"instance_id"`
	UpstreamID string    `json:"upstream_id,omitempty"`
	Ready      bool      `json:"ready"`
	Closed     bool      `json:"closed"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type affinityRPC struct {
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func firstLiveInstance(channel *channelControl) string {
	instances := make([]string, 0, len(channel.Instances))
	for instance := range channel.Instances {
		instances = append(instances, instance)
	}
	sort.Strings(instances)
	return instances[0] // reserveCommand already requires live presence.
}

func (b *Broker) routeAffinity(ctx context.Context, tunnelUUID string, channel *channelControl, command *queuedCommand, clientSessionID string) error {
	var rpc affinityRPC
	if command.CommandType == CommandTypeJSONRPC {
		if err := json.Unmarshal(command.JSONRPC, &rpc); err != nil {
			return err
		}
	}
	command.Headers = command.Headers.Clone()
	if rpc.Method == "initialize" {
		command.SessionID, command.StartsSession = "oma_"+command.RequestID, true
		command.TargetInstance = firstLiveInstance(channel)
		command.Headers.Del("Mcp-Session-Id")
		return nil
	}
	if clientSessionID == "" && (channel.Declaration.Stateless || command.CommandType == CommandTypeOAuthDiscovery) {
		// Self-contained calls have no session identity to migrate. Keep a stable
		// owner for this path; legacy initialize sessions use the mapping below.
		if channel.Owner == "" {
			channel.Owner = firstLiveInstance(channel)
		}
		if _, live := channel.Instances[channel.Owner]; !live {
			return ErrNoConnector
		}
		command.TargetInstance = channel.Owner
		return nil
	}
	session, _, err := b.readSession(ctx, tunnelUUID, clientSessionID)
	if err != nil {
		return err
	}
	if !session.Ready || session.Closed || session.Channel != command.Channel {
		return ErrSessionNotFound
	}
	if _, live := channel.Instances[session.InstanceID]; !live {
		return ErrSessionNotFound
	}
	command.SessionID, command.TargetInstance = session.ID, session.InstanceID
	command.Headers.Del("Mcp-Session-Id")
	if session.UpstreamID != "" {
		command.Headers.Set("Mcp-Session-Id", session.UpstreamID)
	}
	command.LocalClose = command.CommandType == CommandTypeSessionTermination && session.UpstreamID == ""
	return nil
}

func sessionKey(tunnelUUID, sessionID string) string {
	return brokerKey(tunnelUUID, "session", sessionID)
}

func (b *Broker) readSession(ctx context.Context, tunnelUUID, sessionID string) (affinitySession, uint64, error) {
	var session affinitySession
	if sessionID == "" {
		return session, 0, ErrSessionNotFound
	}
	stored, err := b.requests.read(ctx, sessionKey(tunnelUUID, sessionID), &session)
	if errors.Is(err, ErrRequestNotFound) {
		return session, 0, ErrSessionNotFound
	}
	if err != nil {
		return session, 0, err
	}
	if session.TunnelUUID != tunnelUUID || session.ID != sessionID || !b.now().Before(session.ExpiresAt) {
		return session, 0, ErrSessionNotFound
	}
	return session, stored.revision, nil
}

func (b *Broker) updateSession(ctx context.Context, tunnelUUID, sessionID string, change func(*affinitySession) error) error {
	for attempt := range brokerCASAttempts {
		session, revision, err := b.readSession(ctx, tunnelUUID, sessionID)
		if err != nil {
			return err
		}
		if err := change(&session); err != nil {
			return err
		}
		session.ExpiresAt = b.now().Add(brokerRequestRetention(b.cfg.RequestTimeout, b.cfg.TombstoneTTL))
		err = b.requests.update(ctx, sessionKey(tunnelUUID, sessionID), session, revision, maxBrokerValueBytes)
		if !brokerCASConflict(err) {
			return err
		}
		if err := brokerPause(ctx, attempt); err != nil {
			return err
		}
	}
	return ErrBrokerBusy
}

func (b *Broker) reserveSession(ctx context.Context, tunnelUUID string, command queuedCommand) error {
	if command.SessionID == "" {
		return nil
	}
	if command.StartsSession {
		session := affinitySession{TunnelUUID: tunnelUUID, ID: command.SessionID, Channel: command.Channel,
			InstanceID: command.TargetInstance, ExpiresAt: b.now().Add(brokerRequestRetention(b.cfg.RequestTimeout, b.cfg.TombstoneTTL))}
		return b.requests.create(ctx, sessionKey(tunnelUUID, session.ID), session, maxBrokerValueBytes)
	}
	return b.updateSession(ctx, tunnelUUID, command.SessionID, func(session *affinitySession) error {
		if !session.Ready || session.Closed || session.InstanceID != command.TargetInstance {
			return ErrSessionNotFound
		}
		return nil
	})
}

func (b *Broker) sessionResponse(ctx context.Context, record requestRecord, response TunnelResponse) (TunnelResponse, error) {
	if record.SessionID == "" {
		return response, nil
	}
	if record.StartsSession {
		if !response.terminal() {
			return affinityResponseHeaders(response, record.SessionID), nil
		}
		var rpc affinityRPC
		if response.ResponseCode >= 400 || json.Unmarshal(response.JSONResponse, &rpc) != nil || len(rpc.Result) == 0 || (len(rpc.Error) > 0 && string(rpc.Error) != "null") {
			return affinityResponseHeaders(response, ""), nil
		}
		err := b.updateSession(ctx, record.TunnelUUID, record.SessionID, func(session *affinitySession) error {
			if session.Closed || session.InstanceID != record.InstanceID {
				return ErrSessionNotFound
			}
			if !session.Ready {
				session.UpstreamID, session.Ready = response.ResponseHeaders.Get("Mcp-Session-Id"), true
			}
			return nil
		})
		if err != nil {
			return response, err
		}
	} else if record.CommandType == CommandTypeSessionTermination && response.terminal() && response.ResponseCode < 400 {
		if err := b.closeSession(ctx, record.TunnelUUID, record.SessionID); err != nil {
			return response, err
		}
	}
	// Both Core forwarding and durable recovery see the mapped header only after
	// initialize's owner/upstream mapping has committed. Downstream IDs stay private.
	return affinityResponseHeaders(response, record.SessionID), nil
}

func affinityResponseHeaders(response TunnelResponse, sessionID string) TunnelResponse {
	response.ResponseHeaders = response.ResponseHeaders.Clone()
	if response.ResponseHeaders == nil {
		response.ResponseHeaders = make(http.Header)
	}
	response.ResponseHeaders.Del("Mcp-Session-Id")
	if sessionID != "" {
		response.ResponseHeaders.Set("Mcp-Session-Id", sessionID)
	}
	return response
}

func (b *Broker) closeSession(ctx context.Context, tunnelUUID, sessionID string) error {
	return b.updateSession(ctx, tunnelUUID, sessionID, func(session *affinitySession) error { session.Closed = true; return nil })
}

func (b *Broker) completeLocalClose(ctx context.Context, tunnelUUID string, command queuedCommand) error {
	record, revision, err := b.readRequest(ctx, tunnelUUID, command.RequestID)
	if err != nil {
		return err
	}
	if err := b.closeSession(ctx, tunnelUUID, command.SessionID); err != nil {
		return err
	}
	record.State = "completed"
	record.Response = &TunnelResponse{RequestID: command.RequestID, Channel: command.Channel, ResponseType: ResponseTypeSessionTermination, ResponseCode: http.StatusNoContent}
	if err := b.requests.update(ctx, brokerKey(tunnelUUID, command.RequestID), record, revision, maxBrokerValueBytes); err != nil {
		return err
	}
	b.releaseCommand(ctx, tunnelUUID, command.RequestID)
	publishBrokerSignal(b.connection, record.Origin, brokerKey(tunnelUUID, command.RequestID))
	return nil
}
