package managedagentsevents

import "testing"

func TestReferencePersistedEventCategories(t *testing.T) {
	tests := map[string]Category{
		"user.message":                      CategoryInput,
		"user.interrupt":                    CategoryInput,
		"user.custom_tool_result":           CategoryInput,
		"user.tool_confirmation":            CategoryInput,
		"user.define_outcome":               CategoryInput,
		"user.tool_result":                  CategoryInput,
		"agent.message":                     CategoryAgent,
		"agent.thinking":                    CategoryAgent,
		"agent.tool_use":                    CategoryTool,
		"agent.tool_result":                 CategoryTool,
		"agent.mcp_tool_use":                CategoryTool,
		"agent.mcp_tool_result":             CategoryTool,
		"agent.custom_tool_use":             CategoryTool,
		"agent.thread_context_compacted":    CategoryAgent,
		"agent.thread_message_received":     CategoryThreadCoordination,
		"agent.thread_message_sent":         CategoryThreadCoordination,
		"session.status_running":            CategorySessionStatus,
		"session.status_idle":               CategorySessionStatus,
		"session.status_rescheduled":        CategorySessionStatus,
		"session.status_terminated":         CategorySessionStatus,
		"session.deleted":                   CategorySessionStatus,
		"session.updated":                   CategorySessionStatus,
		"session.error":                     CategorySessionStatus,
		"session.thread_created":            CategoryThreadCoordination,
		"session.thread_status_running":     CategoryThreadStatus,
		"session.thread_status_idle":        CategoryThreadStatus,
		"session.thread_status_rescheduled": CategoryThreadStatus,
		"session.thread_status_terminated":  CategoryThreadStatus,
		"span.model_request_start":          CategorySpan,
		"span.model_request_end":            CategorySpan,
		"span.outcome_evaluation_start":     CategorySpan,
		"span.outcome_evaluation_ongoing":   CategorySpan,
		"span.outcome_evaluation_end":       CategorySpan,
		"system.message":                    CategorySystem,
		"event_start":                       CategoryStreamDelta,
		"event_delta":                       CategoryStreamDelta,
	}
	for eventType, want := range tests {
		if got := CategoryFor(eventType); got != want {
			t.Fatalf("CategoryFor(%q) = %q, want %q", eventType, got, want)
		}
		if want != CategoryStreamDelta && !IsPersistedManagedAgentEvent(eventType) {
			t.Fatalf("IsPersistedManagedAgentEvent(%q) = false, want true", eventType)
		}
	}
}

func TestWorkerOutputEventPolicy(t *testing.T) {
	allowed := []string{
		"assistant",
		"user",
		"system",
		"result",
		"agent.message",
		"agent.thinking",
		"agent.tool_use",
		"agent.tool_result",
		"agent.mcp_tool_use",
		"agent.mcp_tool_result",
		"agent.custom_tool_use",
		"agent.thread_context_compacted",
		"agent.thread_message_received",
		"agent.thread_message_sent",
		"session.status_running",
		"session.status_idle",
		"session.status_rescheduled",
		"session.status_terminated",
		"session.deleted",
		"session.updated",
		"session.error",
		"session.thread_created",
		"session.thread_status_running",
		"session.thread_status_idle",
		"session.thread_status_rescheduled",
		"session.thread_status_terminated",
		"span.model_request_start",
		"span.model_request_end",
		"span.outcome_evaluation_start",
		"span.outcome_evaluation_ongoing",
		"span.outcome_evaluation_end",
		"system.message",
	}
	for _, eventType := range allowed {
		if !IsWorkerOutputEvent(eventType) {
			t.Fatalf("IsWorkerOutputEvent(%q) = false, want true", eventType)
		}
	}
	blocked := []string{
		"user.message",
		"user.interrupt",
		"user.custom_tool_result",
		"user.tool_confirmation",
		"user.define_outcome",
		"user.tool_result",
		"env_manager_log",
		"event_start",
		"event_delta",
		"control_request",
	}
	for _, eventType := range blocked {
		if IsWorkerOutputEvent(eventType) {
			t.Fatalf("IsWorkerOutputEvent(%q) = true, want false", eventType)
		}
	}
}

func TestPublicSessionHistoryEventPolicy(t *testing.T) {
	allowed := []string{
		"assistant",
		"result",
		"agent.message",
		"agent.tool_use",
		"session.thread_created",
		"span.model_request_start",
		"system.message",
	}
	for _, eventType := range allowed {
		if !IsPublicSessionHistoryEvent(eventType) {
			t.Fatalf("IsPublicSessionHistoryEvent(%q) = false, want true", eventType)
		}
	}
	blocked := []string{
		"env_manager_log",
		"event_start",
		"event_delta",
		"control_request",
		"",
	}
	for _, eventType := range blocked {
		if IsPublicSessionHistoryEvent(eventType) {
			t.Fatalf("IsPublicSessionHistoryEvent(%q) = true, want false", eventType)
		}
	}
}
