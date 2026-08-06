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

type capabilityValueKind uint8

const (
	capabilityNullKind capabilityValueKind = iota
	capabilityObjectKind
	capabilityArrayKind
	capabilityStringKind
	capabilityNumberKind
	capabilityBooleanKind
)

// CapabilityValue is a structured JSON value used to preserve Gateway
// capability extensions without leaking json.RawMessage into the domain model.
type CapabilityValue struct {
	kind    capabilityValueKind
	object  map[string]CapabilityValue
	array   []CapabilityValue
	string  string
	number  json.Number
	boolean bool
}

type Capabilities map[string]CapabilityValue

func (v *CapabilityValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*v = CapabilityValue{kind: capabilityNullKind}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	parsed, err := capabilityValueFromAny(value)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

func (v CapabilityValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.any())
}

func (v CapabilityValue) any() any {
	switch v.kind {
	case capabilityObjectKind:
		fields := make(map[string]any, len(v.object))
		for name, value := range v.object {
			fields[name] = value.any()
		}
		return fields
	case capabilityArrayKind:
		values := make([]any, 0, len(v.array))
		for _, value := range v.array {
			values = append(values, value.any())
		}
		return values
	case capabilityStringKind:
		return v.string
	case capabilityNumberKind:
		return v.number
	case capabilityBooleanKind:
		return v.boolean
	default:
		return nil
	}
}

func capabilityValueFromAny(value any) (CapabilityValue, error) {
	switch typed := value.(type) {
	case nil:
		return CapabilityValue{kind: capabilityNullKind}, nil
	case map[string]any:
		fields := make(map[string]CapabilityValue, len(typed))
		for name, item := range typed {
			parsed, err := capabilityValueFromAny(item)
			if err != nil {
				return CapabilityValue{}, err
			}
			fields[name] = parsed
		}
		return CapabilityValue{kind: capabilityObjectKind, object: fields}, nil
	case []any:
		values := make([]CapabilityValue, 0, len(typed))
		for _, item := range typed {
			parsed, err := capabilityValueFromAny(item)
			if err != nil {
				return CapabilityValue{}, err
			}
			values = append(values, parsed)
		}
		return CapabilityValue{kind: capabilityArrayKind, array: values}, nil
	case string:
		return CapabilityValue{kind: capabilityStringKind, string: typed}, nil
	case json.Number:
		return CapabilityValue{kind: capabilityNumberKind, number: typed}, nil
	case bool:
		return CapabilityValue{kind: capabilityBooleanKind, boolean: typed}, nil
	default:
		return CapabilityValue{}, fmt.Errorf("unsupported capability value %T", value)
	}
}

func capabilityObjectValue(value CapabilityValue) map[string]CapabilityValue {
	if value.kind != capabilityObjectKind || value.object == nil {
		return make(map[string]CapabilityValue)
	}
	return value.object
}

func cloneCapabilityValue(value CapabilityValue) CapabilityValue {
	cloned := value
	if value.object != nil {
		cloned.object = make(map[string]CapabilityValue, len(value.object))
		for name, item := range value.object {
			cloned.object[name] = cloneCapabilityValue(item)
		}
	}
	if value.array != nil {
		cloned.array = make([]CapabilityValue, len(value.array))
		for index, item := range value.array {
			cloned.array[index] = cloneCapabilityValue(item)
		}
	}
	return cloned
}

type KnownCapabilities struct {
	Batch             *bool
	Citations         *bool
	CodeExecution     *bool
	ContextManagement *bool
	ClearThinking     *bool
	ClearToolUses     *bool
	CompactContext    *bool
	Effort            *bool
	LowEffort         *bool
	MediumEffort      *bool
	HighEffort        *bool
	XHighEffort       *bool
	MaxEffort         *bool
	ImageInput        *bool
	PDFInput          *bool
	StructuredOutputs *bool
	Thinking          *bool
	ThinkingEnabled   *bool
	AdaptiveThinking  *bool
	ToolUse           *bool
}

func (c *Capabilities) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = nil
		return nil
	}
	var fields map[string]CapabilityValue
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("capabilities must be an object: %w", err)
	}
	if err := validateKnownCapabilities(fields); err != nil {
		return err
	}
	*c = Capabilities(cloneCapabilityValues(fields))
	return nil
}

func (c *Capabilities) setSupported(name string, supported *bool) {
	if supported == nil {
		return
	}
	if *c == nil {
		*c = make(Capabilities)
	}
	fields := capabilityObjectValue((*c)[name])
	fields["supported"] = CapabilityValue{kind: capabilityBooleanKind, boolean: *supported}
	(*c)[name] = CapabilityValue{kind: capabilityObjectKind, object: fields}
}

func (c Capabilities) Known() KnownCapabilities {
	thinkingFields := capabilityObject(c["thinking"])
	thinkingTypes := capabilityObject(thinkingFields["types"])
	contextFields := capabilityObject(c["context_management"])
	effortFields := capabilityObject(c["effort"])
	return KnownCapabilities{
		Batch:             capabilitySupported(c["batch"]),
		Citations:         capabilitySupported(c["citations"]),
		CodeExecution:     capabilitySupported(c["code_execution"]),
		ContextManagement: capabilitySupported(c["context_management"]),
		ClearThinking:     capabilitySupported(contextFields["clear_thinking_20251015"]),
		ClearToolUses:     capabilitySupported(contextFields["clear_tool_uses_20250919"]),
		CompactContext:    capabilitySupported(contextFields["compact_20260112"]),
		Effort:            capabilitySupported(c["effort"]),
		LowEffort:         capabilitySupported(effortFields["low"]),
		MediumEffort:      capabilitySupported(effortFields["medium"]),
		HighEffort:        capabilitySupported(effortFields["high"]),
		XHighEffort:       capabilitySupported(effortFields["xhigh"]),
		MaxEffort:         capabilitySupported(effortFields["max"]),
		ImageInput:        capabilitySupported(c["image_input"]),
		PDFInput:          capabilitySupported(c["pdf_input"]),
		StructuredOutputs: capabilitySupported(c["structured_outputs"]),
		Thinking:          capabilitySupported(c["thinking"]),
		ThinkingEnabled:   capabilitySupported(thinkingTypes["enabled"]),
		AdaptiveThinking:  capabilitySupported(thinkingTypes["adaptive"]),
		ToolUse:           capabilitySupported(c["tool_use"]),
	}
}

func validateKnownCapabilities(fields map[string]CapabilityValue) error {
	for _, name := range []string{
		"batch", "citations", "code_execution", "context_management", "effort",
		"image_input", "pdf_input", "structured_outputs", "thinking", "tool_use",
	} {
		if _, err := supportedCapability(fields[name], name); err != nil {
			return err
		}
	}
	thinkingFields := capabilityObject(fields["thinking"])
	thinkingTypes := capabilityObject(thinkingFields["types"])
	for _, name := range []string{"enabled", "adaptive"} {
		if _, err := supportedCapability(thinkingTypes[name], "thinking.types."+name); err != nil {
			return err
		}
	}
	contextFields := capabilityObject(fields["context_management"])
	for _, name := range []string{"clear_thinking_20251015", "clear_tool_uses_20250919", "compact_20260112"} {
		if _, err := supportedCapability(contextFields[name], "context_management."+name); err != nil {
			return err
		}
	}
	effortFields := capabilityObject(fields["effort"])
	for _, name := range []string{"low", "medium", "high", "xhigh", "max"} {
		if _, err := supportedCapability(effortFields[name], "effort."+name); err != nil {
			return err
		}
	}
	return nil
}

func supportedCapability(value CapabilityValue, name string) (*bool, error) {
	if value.kind == capabilityNullKind {
		return nil, nil
	}
	if value.kind != capabilityObjectKind {
		return nil, fmt.Errorf("%s capability must be an object", name)
	}
	supported, ok := value.object["supported"]
	if !ok || supported.kind == capabilityNullKind {
		return nil, nil
	}
	if supported.kind != capabilityBooleanKind {
		return nil, fmt.Errorf("%s.supported must be a boolean", name)
	}
	valueCopy := supported.boolean
	return &valueCopy, nil
}

func capabilitySupported(value CapabilityValue) *bool {
	if value.kind != capabilityObjectKind {
		return nil
	}
	supported, ok := value.object["supported"]
	if !ok || supported.kind != capabilityBooleanKind {
		return nil
	}
	valueCopy := supported.boolean
	return &valueCopy
}

func capabilityObject(value CapabilityValue) map[string]CapabilityValue {
	return capabilityObjectValue(value)
}

func cloneCapabilityValues(fields map[string]CapabilityValue) map[string]CapabilityValue {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]CapabilityValue, len(fields))
	for name, value := range fields {
		cloned[name] = cloneCapabilityValue(value)
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
