package db

import (
	"context"
	"encoding/json"
	"slices"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func prepareDSHCoordinationEvents(ctx context.Context, executor yourbatis.Executor, scope DSHScope, session Session, primary SessionThread, item *DSHPublicEvent) error {
	if !slices.ContainsFunc(item.Events, isDSHCoordinationEvent) {
		return nil
	}
	item.Events = slices.Clone(item.Events)
	var mirrors []SessionEvent
	for i := range item.Events {
		event := &item.Events[i]
		if !isDSHCoordinationEvent(*event) {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(event.Payload, &fields); err != nil {
			return err
		}
		if event.EventType == "session.thread_created" {
			if item.Thread == nil {
				return ErrInvalidState
			}
			parent, err := dshParentThread(ctx, executor, scope, session, primary, item.DSHSessionID)
			if err != nil {
				return err
			}
			fields["parent_thread_id"], _ = json.Marshal(parent.ExternalID)
			fields["agent"] = item.Thread.AgentSnapshot
			if err := setDSHAgentName(fields, "agent_name", item.Thread.AgentSnapshot); err != nil {
				return err
			}
		} else {
			prefix := "to"
			if event.EventType == "agent.thread_message_received" {
				prefix = "from"
			}
			var threadID string
			if err := json.Unmarshal(fields[prefix+"_session_thread_id"], &threadID); err != nil {
				return err
			}
			thread, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, scope.WorkspaceUUID, scope.SessionID, threadID)
			if err != nil {
				return mapNoRows(err)
			}
			if err := setDSHAgentName(fields, prefix+"_agent_name", thread.AgentSnapshot); err != nil {
				return err
			}
			owner := primary
			if event.ThreadExternalID != nil && *event.ThreadExternalID != primary.ExternalID {
				row, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, scope.WorkspaceUUID, scope.SessionID, *event.ThreadExternalID)
				if err != nil {
					return mapNoRows(err)
				}
				owner = row.thread()
			}
			peer := thread.thread()
			if !dshDirectParent(owner, peer) && !dshDirectParent(peer, owner) {
				return ErrInvalidState
			}
			mirror, err := dshThreadMessageMirror(*event, fields, peer, owner)
			if err != nil {
				return err
			}
			mirrors = append(mirrors, mirror)
		}
		payload, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		event.Payload = payload
	}
	item.Events = append(item.Events, mirrors...)
	return nil
}

func isDSHCoordinationEvent(event SessionEvent) bool {
	switch event.EventType {
	case "session.thread_created", "agent.thread_message_sent", "agent.thread_message_received":
		return true
	default:
		return false
	}
}

func dshDirectParent(parent, child SessionThread) bool {
	return child.ParentThreadUUID != nil && *child.ParentThreadUUID == parent.UUID
}

func setDSHAgentName(fields map[string]json.RawMessage, key string, snapshot json.RawMessage) error {
	var agent struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(snapshot, &agent); err != nil {
		return err
	}
	fields[key], _ = json.Marshal(agent.Name)
	return nil
}

func dshThreadMessageMirror(source SessionEvent, fields map[string]json.RawMessage, peer, owner SessionThread) (SessionEvent, error) {
	payload := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		payload[key] = value
	}
	for _, key := range []string{"to_session_thread_id", "to_agent_name", "from_session_thread_id", "from_agent_name", "agent_name"} {
		delete(payload, key)
	}
	kind, prefix := "agent.thread_message_received", "from"
	if source.EventType == "agent.thread_message_received" {
		kind, prefix = "agent.thread_message_sent", "to"
	}
	event := source
	event.UUID = uuid.NewV4().String()
	event.ExternalID = derivedStatusEventID(source.ExternalID, "coordination:"+peer.ExternalID+":"+kind)
	event.EventType = kind
	event.ThreadExternalID = &peer.ExternalID
	event.ThreadUUID = &peer.UUID
	payload["id"], _ = json.Marshal(event.ExternalID)
	payload["type"], _ = json.Marshal(kind)
	payload["original_event_id"], _ = json.Marshal(source.ExternalID)
	payload["session_thread_id"], _ = json.Marshal(peer.ExternalID)
	payload[prefix+"_session_thread_id"], _ = json.Marshal(owner.ExternalID)
	if err := setDSHAgentName(payload, prefix+"_agent_name", owner.AgentSnapshot); err != nil {
		return SessionEvent{}, err
	}
	raw, err := json.Marshal(payload)
	event.Payload = raw
	return event, err
}
