package managedagentsevents

import "strings"

type Category string

const (
	CategoryInput              Category = "input"
	CategoryAgent              Category = "agent"
	CategoryTool               Category = "tool"
	CategorySessionStatus      Category = "session_status"
	CategoryThreadStatus       Category = "thread_status"
	CategoryThreadCoordination Category = "thread_coordination"
	CategorySpan               Category = "span"
	CategorySystem             Category = "system"
	CategoryStreamDelta        Category = "stream_delta"
	CategoryUnknown            Category = "unknown"
)

func CategoryFor(eventType string) Category {
	switch strings.TrimSpace(eventType) {
	case "user.message", "user.interrupt", "user.custom_tool_result", "user.tool_confirmation", "user.define_outcome", "user.tool_result":
		return CategoryInput
	case "agent.message", "agent.thinking", "agent.thread_context_compacted":
		return CategoryAgent
	case "agent.tool_use", "agent.tool_result", "agent.mcp_tool_use", "agent.mcp_tool_result", "agent.custom_tool_use":
		return CategoryTool
	case "agent.thread_message_received", "agent.thread_message_sent", "session.thread_created":
		return CategoryThreadCoordination
	case "session.status_running", "session.status_idle", "session.status_rescheduled", "session.status_terminated",
		"session.status_run_started", "session.status_idled", "session.running", "session.idled", "session.requires_action",
		"session.deleted", "session.updated", "session.error":
		return CategorySessionStatus
	case "session.thread_status_running", "session.thread_status_idle", "session.thread_status_rescheduled", "session.thread_status_terminated",
		"session.thread_idled", "session.thread_terminated":
		return CategoryThreadStatus
	case "span.model_request_start", "span.model_request_end", "span.outcome_evaluation_start", "span.outcome_evaluation_ongoing", "span.outcome_evaluation_end":
		return CategorySpan
	case "system.message":
		return CategorySystem
	case "event_start", "event_delta":
		return CategoryStreamDelta
	default:
		return CategoryUnknown
	}
}

func IsClientInput(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "user.message", "user.interrupt", "user.custom_tool_result", "user.tool_confirmation", "user.define_outcome", "user.tool_result", "system.message":
		return true
	default:
		return false
	}
}

func IsPersistedManagedAgentEvent(eventType string) bool {
	category := CategoryFor(eventType)
	return category != CategoryUnknown && category != CategoryStreamDelta
}

func IsPublicSessionHistoryEvent(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	if eventType == "env_manager_log" || IsStreamDelta(eventType) {
		return false
	}
	return IsPersistedManagedAgentEvent(eventType) || IsClaudeCodeTranscriptEvent(eventType)
}

func IsStreamDelta(eventType string) bool {
	return CategoryFor(eventType) == CategoryStreamDelta
}

func IsClaudeCodeTranscriptEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "assistant", "user", "system", "result":
		return true
	default:
		return false
	}
}

func IsWorkerOutputEvent(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	if IsClaudeCodeTranscriptEvent(eventType) {
		return true
	}
	if IsStreamDelta(eventType) {
		return false
	}
	switch CategoryFor(eventType) {
	case CategoryAgent, CategoryTool, CategorySessionStatus, CategoryThreadStatus, CategoryThreadCoordination, CategorySpan, CategorySystem:
		return true
	default:
		return false
	}
}

func SessionStatus(eventType string) (string, bool) {
	switch strings.TrimSpace(eventType) {
	case "session.status_run_started", "session.status_running", "session.running":
		return "running", true
	case "session.status_rescheduled":
		return "rescheduling", true
	case "session.status_idle", "session.status_idled", "session.idled", "session.requires_action":
		return "idle", true
	case "session.status_terminated", "session.deleted":
		return "terminated", true
	default:
		return "", false
	}
}

func ThreadStatus(eventType string) (string, bool) {
	switch strings.TrimSpace(eventType) {
	case "session.thread_status_running":
		return "running", true
	case "session.thread_status_rescheduled":
		return "rescheduling", true
	case "session.thread_status_idle", "session.thread_idled":
		return "idle", true
	case "session.thread_status_terminated", "session.thread_terminated":
		return "terminated", true
	default:
		return "", false
	}
}

func IsPrimaryCoordinationEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "session.thread_created",
		"session.thread_status_running", "session.thread_status_idle", "session.thread_status_rescheduled", "session.thread_status_terminated",
		"session.thread_idled", "session.thread_terminated",
		"agent.thread_message_received", "agent.thread_message_sent":
		return true
	default:
		return false
	}
}

func IsCrossPostedBlockingEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		return true
	default:
		return false
	}
}
