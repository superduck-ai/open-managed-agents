package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

type History interface {
	Load(context.Context, string) ([]fantasy.Message, error)
	Append(context.Context, string, []fantasy.Message) error
}

type EventSink interface {
	Publish(context.Context, Event) error
}

type EventType string

const (
	EventTextStart      EventType = "text_start"
	EventTextDelta      EventType = "text_delta"
	EventTextEnd        EventType = "text_end"
	EventReasoningStart EventType = "reasoning_start"
	EventReasoningDelta EventType = "reasoning_delta"
	EventReasoningEnd   EventType = "reasoning_end"
	EventToolCall       EventType = "tool_call"
	EventToolResult     EventType = "tool_result"
	EventStepStart      EventType = "step_start"
	EventStepResponse   EventType = "step_response"
	EventStepFinish     EventType = "step_finish"
	EventRetry          EventType = "retry"
	EventRunFinish      EventType = "run_finish"
)

type Event struct {
	Type        EventType
	SessionID   string
	RunID       string
	Step        int
	Attempt     int
	ID          string
	Text        string
	Reasoning   *fantasy.ReasoningContent
	ToolCall    *fantasy.ToolCallContent
	ToolResult  *fantasy.ToolResultContent
	Result      *fantasy.StepResult
	AgentResult *fantasy.AgentResult
	BlockIDs    []string
}

type Options struct {
	Model        fantasy.LanguageModel
	SystemPrompt string
	History      History
	Events       EventSink
	Tools        []fantasy.AgentTool
	MaxSteps     int
	MaxRetries   int
}

type Call struct {
	SessionID       string
	RunID           string
	Prompt          string
	Files           []fantasy.FilePart
	Tools           []fantasy.AgentTool
	ProviderOptions fantasy.ProviderOptions
	MaxOutputTokens *int64
}

type ExecutionError struct {
	Cause error
}

func (e *ExecutionError) Error() string { return e.Cause.Error() }
func (e *ExecutionError) Unwrap() error { return e.Cause }

var (
	ErrBusy         = errors.New("session already has an active run")
	ErrPendingTools = errors.New("session history has unresolved tool calls")
)

type Engine struct {
	opts   Options
	mu     sync.Mutex
	active map[string]*activeRun
}

type activeRun struct {
	cancel context.CancelFunc
}

func New(opts Options) (*Engine, error) {
	if opts.Model == nil || opts.History == nil || opts.Events == nil {
		return nil, errors.New("model, history and events are required")
	}
	if opts.MaxSteps < 0 || opts.MaxRetries < 0 {
		return nil, errors.New("step and retry limits must be nonnegative")
	}
	if opts.MaxSteps == 0 {
		opts.MaxSteps = 64
	}
	opts.Tools = append([]fantasy.AgentTool(nil), opts.Tools...)
	return &Engine{opts: opts, active: make(map[string]*activeRun)}, nil
}

func (e *Engine) Cancel(sessionID string) {
	e.mu.Lock()
	run := e.active[sessionID]
	e.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

func (e *Engine) Run(ctx context.Context, call Call) (*fantasy.AgentResult, error) {
	if call.SessionID == "" || call.RunID == "" {
		return nil, errors.New("session and run IDs are required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &activeRun{cancel: cancel}
	e.mu.Lock()
	if e.active[call.SessionID] != nil {
		e.mu.Unlock()
		cancel()
		return nil, ErrBusy
	}
	e.active[call.SessionID] = run
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		if e.active[call.SessionID] == run {
			delete(e.active, call.SessionID)
		}
		e.mu.Unlock()
	}()
	history, err := e.opts.History.Load(runCtx, call.SessionID)
	if err != nil {
		return nil, fmt.Errorf("load agent history: %w", err)
	}
	if hasPendingTools(history) {
		return nil, ErrPendingTools
	}
	if call.Prompt != "" || len(call.Files) > 0 {
		input := fantasy.NewUserMessage(call.Prompt, call.Files...)
		if err := e.opts.History.Append(runCtx, call.SessionID, []fantasy.Message{input}); err != nil {
			return nil, fmt.Errorf("persist agent input: %w", err)
		}
	}
	tools := e.opts.Tools
	if call.Tools != nil {
		tools = call.Tools
	}
	state := &runState{engine: e, call: call, ctx: runCtx, cancel: cancel}
	tools = state.wrapTools(tools)
	projection := newProjection(call.SessionID, history)
	model := agent.Model{Model: &guardedModel{LanguageModel: e.opts.Model, state: state, tools: tools}, CatwalkCfg: catwalk.Model{SupportsImages: true}, ModelCfg: config.SelectedModel{Provider: "anthropic", Model: e.opts.Model.Model()}}
	sessionAgent := agent.NewSessionAgent(agent.SessionAgentOptions{
		LargeModel: model, SmallModel: model, SystemPrompt: e.opts.SystemPrompt,
		Sessions: projection.sessions, Messages: projection.messages, Tools: tools,
		DisableTitleGeneration: true, DisableTodoReminder: true, IsolatedMCP: true,
		AllowAttachmentOnly:      true,
		PreservePendingToolCalls: true, StreamCall: state.decorate,
	})
	var attachments []message.Attachment
	for _, file := range call.Files {
		attachments = append(attachments, message.Attachment{MimeType: file.MediaType, Content: file.Data, FileName: file.Filename})
	}
	var maxOutputTokens int64
	if call.MaxOutputTokens != nil {
		maxOutputTokens = *call.MaxOutputTokens
	}
	result, err := sessionAgent.Run(runCtx, agent.SessionAgentCall{SessionID: call.SessionID, RunID: call.RunID, Prompt: call.Prompt, Attachments: attachments, ProviderOptions: call.ProviderOptions, MaxOutputTokens: maxOutputTokens, NonInteractive: true})
	if failure := state.err(); failure != nil {
		return result, failure
	}
	return result, err
}

func hasPendingTools(messages []fantasy.Message) bool {
	pending := make(map[string]struct{})
	for _, message := range messages {
		for _, part := range message.Content {
			if call, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok && !call.ProviderExecuted {
				pending[call.ToolCallID] = struct{}{}
			}
			if result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part); ok {
				delete(pending, result.ToolCallID)
			}
		}
	}
	return len(pending) > 0
}
