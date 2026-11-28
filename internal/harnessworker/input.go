package harnessworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const (
	legacyTextVersion  = 2
	legacyTaskVersion  = 3
	deliveryVersion    = 4
	userInputKind      = "user_input"
	choiceResponseKind = db.HarnessChoiceResponse
	toolResultKind     = db.HarnessToolResult
)

type deliveryEnvelope struct {
	Version      int                `json:"version"`
	Turn         db.HarnessTurnRef  `json:"turn"`
	Kind         string             `json:"kind,omitempty"`
	TaskUUID     string             `json:"task_uuid,omitempty"`
	ToolUUID     string             `json:"tool_uuid,omitempty"`
	Input        *textInput         `json:"input,omitempty"`
	Continuation *modelContinuation `json:"continuation,omitempty"`
}

type textInput struct {
	GroupID  cloudharness.ThreadID `json:"group_id"`
	Text     string                `json:"text"`
	Metadata json.RawMessage       `json:"metadata,omitempty"`
}

type modelContinuation struct {
	Result    cloudharness.ToolResult `json:"result"`
	ToolCount int                     `json:"tool_count"`
}

type natsInputSender struct {
	js    jetstream.JetStream
	scope db.HarnessTurnRef
}

func (q *Queue) InputSender(session db.Session) cloudharness.InputSender {
	return natsInputSender{js: q.js, scope: db.HarnessTurnRef{
		OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID,
	}}
}

func (s natsInputSender) SendToInputQueue(ctx context.Context, message cloudharness.InputMessage, dedupID string) error {
	ref := s.scope
	ref.UUID = dedupID
	if message.Synthetic || message.GroupID != cloudharness.ThreadID(ref.SessionUUID) {
		return errors.New("invalid harness input group or origin")
	}
	envelope := deliveryEnvelope{Version: deliveryVersion, Turn: ref}
	switch content := message.Content.(type) {
	case cloudharness.UserMessage:
		envelope.Kind = userInputKind
		envelope.Input = &textInput{GroupID: message.GroupID, Text: content.Text, Metadata: message.Metadata}
	case cloudharness.ChoiceResponse:
		envelope.Kind = choiceResponseKind
	case cloudharness.ToolResultReady:
		envelope.Kind = toolResultKind
	default:
		return errors.New("unsupported harness NATS input")
	}
	return publishDelivery(ctx, s.js, envelope)
}

func publishDelivery(ctx context.Context, js jetstream.JetStream, envelope deliveryEnvelope) error {
	if err := envelope.validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	messageID := envelope.Turn.UUID
	if kind := envelope.resumeKind(); kind != "" {
		messageID = kind + ":" + messageID
	}
	_, err = js.Publish(ctx, taskSubject, payload, jetstream.WithMsgID(messageID))
	return err
}

func decodeDelivery(data []byte) (deliveryEnvelope, error) {
	var envelope deliveryEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return deliveryEnvelope{}, errors.New("invalid harness delivery")
	}
	return envelope, envelope.validate()
}

func (e deliveryEnvelope) validate() error {
	ref := e.Turn
	for _, id := range []string{ref.UUID, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID} {
		if !canonicalUUID(id) {
			return errors.New("invalid harness delivery scope")
		}
	}
	if e.Version == deliveryVersion {
		return e.validateTask()
	}
	if e.Kind != "" || e.ToolUUID != "" || e.Continuation != nil {
		return errors.New("unexpected fields on legacy harness delivery")
	}
	switch e.Version {
	case 1:
		if e.Input != nil || e.TaskUUID != "" {
			return errors.New("invalid legacy harness input")
		}
	case legacyTextVersion:
		if e.TaskUUID != "" || !e.validText() {
			return errors.New("invalid harness text input")
		}
	case legacyTaskVersion:
		if !canonicalUUID(e.TaskUUID) || e.Input != nil {
			return errors.New("invalid harness task")
		}
	default:
		return fmt.Errorf("unsupported harness input version %d", e.Version)
	}
	return nil
}

func (e deliveryEnvelope) validateTask() error {
	if e.resumeKind() != "" && e.TaskUUID == "" && e.ToolUUID == "" && e.Input == nil && e.Continuation == nil {
		return nil
	}
	if e.Kind == userInputKind {
		if e.TaskUUID != "" || e.ToolUUID != "" || e.Continuation != nil || !e.validText() {
			return errors.New("invalid harness user input delivery")
		}
		return nil
	}
	if !canonicalUUID(e.TaskUUID) || !canonicalUUID(e.ToolUUID) || e.Input != nil {
		return errors.New("invalid harness task reference")
	}
	switch e.Kind {
	case db.HarnessSubagentTask:
		if e.Continuation == nil && e.TaskUUID != e.ToolUUID {
			return nil
		}
	case db.HarnessToolTask:
		if e.TaskUUID == e.ToolUUID && e.Continuation == nil {
			return nil
		}
	case db.HarnessModelTask:
		if e.Continuation != nil && e.Continuation.Result.CallID != "" &&
			len(e.Continuation.Result.Text) <= sessioncontract.HarnessToolResultMaxBytes &&
			e.Continuation.ToolCount > 0 && e.Continuation.ToolCount <= db.HarnessMaxToolCalls {
			return nil
		}
	}
	return errors.New("invalid harness task delivery")
}

func (e deliveryEnvelope) validText() bool {
	return e.Input != nil && e.Input.GroupID == cloudharness.ThreadID(e.Turn.SessionUUID) &&
		len(e.Input.Text) > 0 && len(e.Input.Text) <= sessioncontract.HarnessInputMaxBytes
}

func canonicalUUID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func (e deliveryEnvelope) claim() db.HarnessTurnDelivery {
	kind := e.Kind
	if kind == userInputKind {
		kind = ""
	}
	return db.HarnessTurnDelivery{InputUUID: e.Turn.UUID, TaskUUID: e.TaskUUID, ToolUUID: e.ToolUUID, Kind: kind, LegacyInput: e.Version == 1}
}

func (e deliveryEnvelope) resumeKind() string {
	switch e.Kind {
	case choiceResponseKind, toolResultKind:
		return e.Kind
	default:
		return ""
	}
}

func (e deliveryEnvelope) stepInput(legacy json.RawMessage, dedupID string) (cloudharness.StepInput, error) {
	text := e.Input
	if text == nil {
		value, err := sessioncontract.ParseHarnessUserInput(legacy)
		if err != nil {
			return cloudharness.StepInput{}, err
		}
		text = &textInput{GroupID: cloudharness.ThreadID(e.Turn.SessionUUID), Text: value.Text, Metadata: value.Metadata}
	}
	return cloudharness.StepInput{
		Message: cloudharness.InputMessage{GroupID: text.GroupID, Content: cloudharness.UserMessage{Text: text.Text}, Metadata: text.Metadata},
		DedupID: dedupID,
	}, nil
}
