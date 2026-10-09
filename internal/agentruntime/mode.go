package agentruntime

import (
	"context"
	"encoding/json"
)

type Mode string

const (
	Sandbox Mode = "sandbox"
	Host    Mode = "host"
)

type StartInput struct {
	CodeSessionID    string
	WorkerEpoch      int64
	OAuthAccessToken string
	SessionConfig    json.RawMessage
	MCPEndpoint      string
	MCPToken         string
}

type Starter interface {
	Start(context.Context, StartInput) error
	Stop(string)
}

func ResolveMode(snapshot json.RawMessage, fallback string) (Mode, error) {
	var input struct {
		Metadata struct {
			Mode string `json:"agent_runtime_mode"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(snapshot, &input); err != nil {
		return "", err
	}
	mode := input.Metadata.Mode
	if mode == "" {
		mode = fallback
	}
	if mode == "" {
		mode = string(Sandbox)
	}
	if mode != string(Sandbox) && mode != string(Host) {
		return "", ErrInvalidMode
	}
	return Mode(mode), nil
}

func PersistedMode(metadata json.RawMessage) (Mode, error) {
	var input struct {
		Config struct {
			Mode Mode `json:"agent_runtime_mode"`
		} `json:"config"`
	}
	if err := json.Unmarshal(metadata, &input); err != nil {
		return "", err
	}
	if input.Config.Mode == "" {
		return Sandbox, nil
	}
	if input.Config.Mode != Sandbox && input.Config.Mode != Host {
		return "", ErrInvalidPersistedMode
	}
	return input.Config.Mode, nil
}
