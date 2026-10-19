package codesessions

import (
	"cmp"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type subagentLifecycle struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	TaskType   string `json:"task_type"`
	TaskID     string `json:"task_id"`
	ToolUseID  string `json:"tool_use_id"`
	ThreadID   string `json:"session_thread_id"`
	AgentID    string `json:"agent_id"`
	AgentIDAlt string `json:"agentId"`
}

func isSubagentTaskType(taskType string) bool {
	switch taskType {
	case "local_agent", "remote_agent", "in_process_teammate":
		return true
	default:
		return false
	}
}

func (l subagentLifecycle) addMappings(mappings map[string]string) {
	if l.ThreadID == "" || ((l.Subtype == "task_started" || l.TaskType != "") && !isSubagentTaskType(l.TaskType)) {
		return
	}
	for _, id := range []string{l.ThreadID, l.TaskID, l.AgentID, l.AgentIDAlt, l.ToolUseID} {
		if id != "" {
			mappings[id] = l.ThreadID
		}
	}
}

func (s *Service) enrichSubagentLifecycle(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) ([]json.RawMessage, error) {
	var mappings map[string]string
	result := make([]json.RawMessage, 0, len(payloads))
	for _, raw := range payloads {
		var lifecycle subagentLifecycle
		if err := jsonv2.Unmarshal(raw, &lifecycle); err != nil {
			return nil, err
		}
		if (lifecycle.TaskID == "" && lifecycle.ToolUseID == "") || (lifecycle.ThreadID == "" && lifecycle.Subtype != "task_notification") {
			result = append(result, raw)
			continue
		}
		if mappings == nil {
			var err error
			mappings, err = s.subagentThreadMappings(ctx, codeSession)
			if err != nil {
				return nil, err
			}
		}
		previous := cmp.Or(mappings[lifecycle.TaskID], mappings[lifecycle.ToolUseID])
		if lifecycle.Subtype == "task_notification" && previous == "" {
			continue
		}
		if previous != "" && previous != lifecycle.ThreadID {
			fields, err := decodeRawJSONObject(raw)
			if err != nil {
				return nil, err
			}
			setRawJSONField(fields, "session_thread_id", previous)
			raw, err = jsonv2.Marshal(fields)
			if err != nil {
				return nil, err
			}
			lifecycle.ThreadID = previous
		}
		if lifecycle.Type == "session.thread_created" {
			lifecycle.addMappings(mappings)
		}
		result = append(result, raw)
	}
	return result, nil
}
