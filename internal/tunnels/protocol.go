package tunnels

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type CommandType string

const (
	CommandTypeJSONRPC            CommandType = "jsonrpc"
	CommandTypeOAuthDiscovery     CommandType = "oauth_discovery"
	CommandTypeSessionTermination CommandType = "session_termination"
)

type ResponseType string

const (
	ResponseTypeJSONRPC            ResponseType = "jsonrpc_response"
	ResponseTypeJSONRPCNotify      ResponseType = "jsonrpc_notify"
	ResponseTypeNotifyAck          ResponseType = "notify_ack"
	ResponseTypeOAuth              ResponseType = "oauth_discovery_response"
	ResponseTypeSessionTermination ResponseType = "session_termination_response"
)

type ChannelDeclaration struct {
	Name            string `json:"name"`
	ProcessAffinity bool   `json:"proc_affinity,omitempty"`
}

type MCPServerInfo struct {
	Version  int                  `json:"version"`
	Channels []ChannelDeclaration `json:"channels"`
}

type queuedCommand struct {
	RequestID   string          `json:"request_id"`
	CommandType CommandType     `json:"command_type"`
	Channel     string          `json:"channel"`
	CreatedAt   time.Time       `json:"created_at"`
	Headers     http.Header     `json:"headers"`
	JSONRPC     json.RawMessage `json:"jsonrpc,omitempty"`
	ExpiresAt   time.Time       `json:"expires_at"`
	ExpiresAtMS int64           `json:"expires_at_unix_ms"`
	PayloadSize int64           `json:"payload_size"`
	AffinityKey string          `json:"affinity_key,omitempty"`
}

type brokerRequestState struct {
	queuedCommand
	State        string          `json:"state"`
	InstanceID   string          `json:"instance_id,omitempty"`
	ShardToken   string          `json:"shard_token,omitempty"`
	TokenVersion int64           `json:"token_version,omitempty"`
	Response     *TunnelResponse `json:"response,omitempty"`
}

type ClaimedCommand struct {
	RequestID       string
	ShardToken      string
	CommandType     CommandType
	Channel         string
	CreatedAt       time.Time
	Headers         http.Header
	JSONRPC         json.RawMessage
	ResponseTimeout time.Duration
}

type TunnelResponse struct {
	RequestID       string          `json:"request_id"`
	Channel         string          `json:"channel,omitempty"`
	JSONResponse    json.RawMessage `json:"resp_json,omitempty"`
	ResponseHeaders http.Header     `json:"resp_headers,omitempty"`
	ResponseCode    int             `json:"resp_code,omitempty"`
	ResponseType    ResponseType    `json:"resp_type,omitempty"`
}

func (r TunnelResponse) terminal() bool {
	return r.ResponseType != ResponseTypeJSONRPCNotify
}

var (
	channelNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	requestIDPattern   = regexp.MustCompile(`^req_[0-9A-Za-z]{24}$`)
	tunnelIDPattern    = regexp.MustCompile(`^tunnel_[0-9a-f]{32}$`)
)

func ParseMCPServerInfo(raw string) ([]ChannelDeclaration, error) {
	if raw == "" {
		return []ChannelDeclaration{{Name: "main"}}, nil
	}
	if len(raw) > 4096 {
		return nil, errors.New("X-Tunnel-MCP-Server-Info exceeds 4096 bytes")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var info MCPServerInfo
	if err := decoder.Decode(&info); err != nil {
		return nil, errors.New("X-Tunnel-MCP-Server-Info must be valid version 1 JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("X-Tunnel-MCP-Server-Info must contain one JSON object")
	}
	if info.Version != 1 || len(info.Channels) == 0 || len(info.Channels) > maxTunnelChannels {
		return nil, errors.New("X-Tunnel-MCP-Server-Info must declare version 1 and 1-32 channels")
	}
	seen := make(map[string]struct{}, len(info.Channels))
	for _, channel := range info.Channels {
		if !channelNamePattern.MatchString(channel.Name) {
			return nil, fmt.Errorf("invalid tunnel channel %q", channel.Name)
		}
		if _, duplicate := seen[channel.Name]; duplicate {
			return nil, fmt.Errorf("duplicate tunnel channel %q", channel.Name)
		}
		seen[channel.Name] = struct{}{}
	}
	return info.Channels, nil
}

func (command ClaimedCommand) MarshalWireJSON() (json.RawMessage, error) {
	timeout := strconvResponseTimeout(command.ResponseTimeout)
	value := struct {
		RequestID       string          `json:"request_id"`
		ShardToken      string          `json:"shard_token"`
		CommandType     CommandType     `json:"command_type"`
		Channel         string          `json:"channel,omitempty"`
		CreatedAt       time.Time       `json:"created_at"`
		Headers         http.Header     `json:"headers"`
		ResponseTimeout *string         `json:"response_timeout,omitempty"`
		JSONRPC         json.RawMessage `json:"jsonrpc,omitempty"`
	}{
		RequestID: command.RequestID, ShardToken: command.ShardToken,
		CommandType: command.CommandType, Channel: command.Channel,
		CreatedAt: command.CreatedAt, Headers: command.Headers,
		ResponseTimeout: &timeout, JSONRPC: command.JSONRPC,
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode polled tunnel command: %w", err)
	}
	return data, nil
}

func strconvResponseTimeout(duration time.Duration) string {
	if duration <= 0 {
		return "0ms"
	}
	milliseconds := duration.Milliseconds()
	if milliseconds == 0 {
		milliseconds = 1
	}
	return fmt.Sprintf("%dms", milliseconds)
}
