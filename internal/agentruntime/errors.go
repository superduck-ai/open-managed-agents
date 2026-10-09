package agentruntime

import "errors"

var (
	ErrInvalidMode          = errors.New("agent_runtime_mode must be sandbox or host")
	ErrInvalidPersistedMode = errors.New("persisted agent runtime mode is invalid")
	ErrInvalidInput         = errors.New("host agent input is invalid or unsupported")
	ErrInvalidConfig        = errors.New("host agent configuration is invalid or unsupported")
	ErrInvalidCatalog       = errors.New("sandbox MCP tool catalog is invalid or unsupported")
	ErrMCPUnavailable       = errors.New("sandbox MCP service is unavailable")
	ErrMCPOutcomeUnknown    = errors.New("sandbox tool execution outcome is unknown")
	ErrExecutionFailed      = errors.New("host agent execution failed")
	ErrInputAlreadyAccepted = errors.New("host agent input was already accepted")
	ErrPendingRun           = errors.New("host agent has an interrupted run; automatic replay is disabled")
	ErrToolNotExecuted      = errors.New("tool execution did not start")
	ErrInterrupted          = errors.New("host agent execution was interrupted")
)
