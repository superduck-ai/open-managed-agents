package fantasyadapter

import (
	"encoding/json"
	"errors"
	"strings"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

type toolInput struct {
	call      harness.ToolCall
	arguments strings.Builder
	ended     bool
	confirmed bool
}

func (t *toolInput) start(id, name string) error {
	if t.call.ID != "" {
		return errors.New("harness supports one client tool call per completion")
	}
	if id == "" || name == "" {
		return errors.New("tool input started without an ID or name")
	}
	t.call.ID, t.call.Name = harness.ToolCallID(id), name
	return nil
}

func (t *toolInput) delta(id, delta string) error {
	if id == "" || t.call.ID != harness.ToolCallID(id) || t.ended {
		return errors.New("tool input delta has no open block")
	}
	t.arguments.WriteString(delta)
	return nil
}

func (t *toolInput) end(id string) error {
	if id == "" || t.call.ID != harness.ToolCallID(id) || t.ended {
		return errors.New("tool input end has no open block")
	}
	t.ended = true
	return nil
}

func (t *toolInput) confirm(call harness.ToolCall) error {
	if t.confirmed {
		return errors.New("harness supports one client tool call per completion")
	}
	if call.ID == "" || call.Name == "" || !json.Valid([]byte(call.Arguments)) {
		return errors.New("incomplete tool call in model stream")
	}
	if t.call.ID != "" && (!t.ended || t.call.ID != call.ID || t.call.Name != call.Name) {
		return errors.New("completed tool call does not match its input block")
	}
	if t.arguments.Len() > 0 {
		call.Arguments = t.arguments.String()
		if !json.Valid([]byte(call.Arguments)) {
			return errors.New("invalid tool input arguments")
		}
	}
	t.call = call
	t.ended, t.confirmed = true, true
	return nil
}
