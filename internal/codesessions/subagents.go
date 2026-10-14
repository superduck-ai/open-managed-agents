package codesessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type subagentLifecycle struct {
	TaskID     string `json:"task_id"`
	ToolUseID  string `json:"tool_use_id"`
	ThreadID   string `json:"session_thread_id"`
	AgentID    string `json:"agent_id"`
	AgentIDAlt string `json:"agentId"`
}

func (s *Service) enrichSubagentLifecycle(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) ([]json.RawMessage, error) {
	var mappings map[string]string
	for i, raw := range payloads {
		var lifecycle subagentLifecycle
		if err := jsonv2.Unmarshal(raw, &lifecycle); err != nil {
			return nil, err
		}
		if lifecycle.TaskID == "" || lifecycle.ThreadID == "" {
			continue
		}
		if mappings == nil {
			var err error
			mappings, err = s.subagentThreadMappings(ctx, codeSession)
			if err != nil {
				return nil, err
			}
		}
		previous := mappings[lifecycle.TaskID]
		if previous == "" || previous == lifecycle.ThreadID {
			continue
		}
		fields, err := decodeRawJSONObject(raw)
		if err != nil {
			return nil, err
		}
		setRawJSONField(fields, "session_thread_id", previous)
		payloads[i], err = jsonv2.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	return payloads, nil
}
