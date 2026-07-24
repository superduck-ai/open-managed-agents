package modelcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const GlobalCatalogKey = "default"

var (
	ErrUnavailable       = errors.New("model catalog is unavailable")
	ErrUnknownModel      = errors.New("model is not in the catalog")
	ErrRefreshInProgress = errors.New("model catalog refresh is already in progress")
)

type Capabilities struct {
	Thinking         *bool `json:"-"`
	AdaptiveThinking *bool `json:"-"`
	ToolUse          *bool `json:"-"`
	fields           map[string]json.RawMessage
}

type capabilityPayload struct {
	Supported *bool                      `json:"supported"`
	Types     map[string]json.RawMessage `json:"types"`
}

func (c *Capabilities) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = Capabilities{}
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("capabilities must be an object: %w", err)
	}
	parsed := Capabilities{fields: cloneCapabilityFields(fields)}
	var err error
	parsed.Thinking, parsed.AdaptiveThinking, err = thinkingCapabilities(fields["thinking"])
	if err != nil {
		return err
	}
	parsed.ToolUse, err = supportedCapability(fields["tool_use"], "tool_use")
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

func (c Capabilities) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.mergedFields())
}

func (c Capabilities) RawJSON() json.RawMessage {
	fields := c.mergedFields()
	if len(fields) == 0 {
		return nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil
	}
	return encoded
}

func (c *Capabilities) setSupported(name string, supported *bool) {
	if supported == nil {
		return
	}
	if c.fields == nil {
		c.fields = make(map[string]json.RawMessage)
	}
	c.fields[name] = mergeSupportedCapability(c.fields[name], supported)
	switch name {
	case "thinking":
		c.Thinking = cloneBool(supported)
	case "tool_use":
		c.ToolUse = cloneBool(supported)
	}
}

func (c Capabilities) mergedFields() map[string]json.RawMessage {
	fields := cloneCapabilityFields(c.fields)
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	if c.Thinking != nil {
		fields["thinking"] = mergeSupportedCapability(fields["thinking"], c.Thinking)
	}
	if c.AdaptiveThinking != nil {
		fields["thinking"] = mergeNestedSupportedCapability(fields["thinking"], "adaptive", c.AdaptiveThinking)
	}
	if c.ToolUse != nil {
		fields["tool_use"] = mergeSupportedCapability(fields["tool_use"], c.ToolUse)
	}
	return fields
}

func thinkingCapabilities(raw json.RawMessage) (*bool, *bool, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil, nil
	}
	var payload capabilityPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("thinking capability must be an object: %w", err)
	}
	adaptive, err := supportedCapability(payload.Types["adaptive"], "thinking.types.adaptive")
	if err != nil {
		return nil, nil, err
	}
	return cloneBool(payload.Supported), adaptive, nil
}

func supportedCapability(raw json.RawMessage, name string) (*bool, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var payload capabilityPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("%s capability must be an object: %w", name, err)
	}
	return cloneBool(payload.Supported), nil
}

func mergeSupportedCapability(raw json.RawMessage, supported *bool) json.RawMessage {
	fields := capabilityObject(raw)
	fields["supported"], _ = json.Marshal(*supported)
	encoded, _ := json.Marshal(fields)
	return encoded
}

func mergeNestedSupportedCapability(raw json.RawMessage, name string, supported *bool) json.RawMessage {
	fields := capabilityObject(raw)
	types := capabilityObject(fields["types"])
	types[name] = mergeSupportedCapability(types[name], supported)
	fields["types"], _ = json.Marshal(types)
	encoded, _ := json.Marshal(fields)
	return encoded
}

func capabilityObject(raw json.RawMessage) map[string]json.RawMessage {
	fields := make(map[string]json.RawMessage)
	_ = json.Unmarshal(raw, &fields)
	return fields
}

func cloneCapabilityFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]json.RawMessage, len(fields))
	for name, value := range fields {
		cloned[name] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

type Model struct {
	ID             string       `json:"id"`
	DisplayName    string       `json:"display_name"`
	Description    string       `json:"description,omitempty"`
	CreatedAt      string       `json:"created_at,omitempty"`
	MaxInputTokens *int         `json:"max_input_tokens,omitempty"`
	MaxTokens      *int         `json:"max_tokens,omitempty"`
	Capabilities   Capabilities `json:"capabilities,omitempty"`
}

type Snapshot struct {
	Models           []Model    `json:"models"`
	DefaultModelID   string     `json:"default_model_id,omitempty"`
	LastAttemptAt    *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	Stale            bool       `json:"stale"`
	DefaultAvailable bool       `json:"default_available"`
}

type StoredSnapshot struct {
	Models        []Model
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	LastError     string
}

type Store interface {
	Load(context.Context) (StoredSnapshot, bool, error)
	SaveSuccess(context.Context, StoredSnapshot) error
	RecordFailure(context.Context, time.Time, string) error
}

type RefreshLocker interface {
	TryAcquireRefresh(context.Context) (release func(), acquired bool, err error)
}

type Page struct {
	Models  []Model
	HasMore bool
	LastID  string
}

type Upstream interface {
	List(context.Context, string) (Page, error)
}

type Reader interface {
	Snapshot(context.Context) (Snapshot, error)
	ValidateModel(context.Context, string) error
}

type Refresher interface {
	TryRefresh(context.Context) error
}

type UnavailableReader struct{}

func (UnavailableReader) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{}, ErrUnavailable
}

func (UnavailableReader) ValidateModel(context.Context, string) error {
	return ErrUnavailable
}

type Options struct {
	DefaultModelID  string
	RefreshInterval time.Duration
	RefreshTimeout  time.Duration
	Now             func() time.Time
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func IsUnknownModel(err error) bool {
	return errors.Is(err, ErrUnknownModel)
}
