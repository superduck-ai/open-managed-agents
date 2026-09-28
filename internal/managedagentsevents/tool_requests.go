package managedagentsevents

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
)

const ToolPermissionRequestMetadataKey = "managed_agent_tool_permission_request"

type toolPermissionRequestMetadata struct {
	PublicEventID   string `json:"public_event_id"`
	RequestID       string `json:"request_id"`
	ToolUseID       string `json:"provider_tool_use_id"`
	SessionThreadID string `json:"session_thread_id"`
}

// PendingToolEventIDs is shared by locked input acceptance and public status.
// An empty threadID selects all threads; requests without a thread belong to primary.
func PendingToolEventIDs(raw []byte, primaryID, threadID string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var metadata map[string]json.RawMessage
	if err := jsonv2.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for key, value := range metadata {
		suffix, prefixed := strings.CutPrefix(key, ToolPermissionRequestMetadataKey+":")
		if key != ToolPermissionRequestMetadataKey && (!prefixed || suffix == "") {
			continue
		}
		var request toolPermissionRequestMetadata
		if err := jsonv2.Unmarshal(value, &request); err != nil {
			return nil, err
		}
		if request.PublicEventID == "" || request.RequestID == "" || request.ToolUseID == "" ||
			(prefixed && suffix != request.PublicEventID) {
			continue
		}
		if key == ToolPermissionRequestMetadataKey {
			if _, keyed := metadata[ToolPermissionRequestMetadataKey+":"+request.PublicEventID]; keyed {
				continue
			}
		}
		if request.SessionThreadID == "" {
			request.SessionThreadID = primaryID
		}
		if threadID == "" || request.SessionThreadID == threadID {
			ids = append(ids, request.PublicEventID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}
