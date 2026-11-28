package cloudharness

type AgentEventKind uint8

const (
	CompletionStarted AgentEventKind = iota
	CompletionFinished
	ToolApprovalRequested
	ToolExecutionStarted
)

type AgentEvent struct {
	Kind  AgentEventKind
	Call  ToolCall
	Usage *Usage
}
