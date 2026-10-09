package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
)

var errProjectionOperation = errors.New("operation is outside the host session projection")

type projection struct {
	sessions *projectedSessions
	messages *projectedMessages
}

type projectedSessions struct {
	*pubsub.Broker[session.Session]
	mu      sync.Mutex
	current session.Session
}

type projectedMessages struct {
	*pubsub.Broker[message.Message]
	mu        sync.Mutex
	sessionID string
	messages  []message.Message
}

func newProjection(sessionID string, history []fantasy.Message) projection {
	store := &projectedMessages{Broker: pubsub.NewBroker[message.Message](), sessionID: sessionID}
	for index, entry := range history {
		store.messages = append(store.messages, restoreMessage(sessionID, index, entry))
	}
	return projection{sessions: &projectedSessions{Broker: pubsub.NewBroker[session.Session](), current: session.Session{ID: sessionID}}, messages: store}
}

func restoreMessage(sessionID string, index int, entry fantasy.Message) message.Message {
	result := message.Message{ID: fmt.Sprintf("history-%d", index), SessionID: sessionID, Role: message.MessageRole(entry.Role), ModelHistory: &entry}
	for _, part := range entry.Content {
		switch value := part.(type) {
		case fantasy.TextPart:
			result.Parts = append(result.Parts, message.TextContent{Text: value.Text})
		case fantasy.ReasoningPart:
			result.Parts = append(result.Parts, message.ReasoningContent{Thinking: value.Text})
		case fantasy.FilePart:
			result.Parts = append(result.Parts, message.BinaryContent{Data: value.Data, MIMEType: value.MediaType})
		case fantasy.ToolCallPart:
			result.Parts = append(result.Parts, message.ToolCall{ID: value.ToolCallID, Name: value.ToolName, Input: value.Input, ProviderExecuted: value.ProviderExecuted, Finished: true})
		case fantasy.ToolResultPart:
			result.Parts = append(result.Parts, message.ToolResult{ToolCallID: value.ToolCallID})
		}
	}
	return result
}

func (p *projectedMessages) Create(_ context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sessionID != p.sessionID {
		return message.Message{}, errProjectionOperation
	}
	created := message.Message{ID: fmt.Sprintf("message-%d", len(p.messages)), SessionID: sessionID, Role: params.Role, Parts: slices.Clone(params.Parts), Model: params.Model, Provider: params.Provider, IsSummaryMessage: params.IsSummaryMessage}
	p.messages = append(p.messages, created)
	return created, nil
}

func (p *projectedMessages) Update(_ context.Context, updated message.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index, entry := range p.messages {
		if updated.SessionID == p.sessionID && entry.ID == updated.ID {
			updated.Parts = slices.Clone(updated.Parts)
			p.messages[index] = updated
			return nil
		}
	}
	return errProjectionOperation
}

func (p *projectedMessages) Get(ctx context.Context, id string) (message.Message, error) {
	entries, err := p.List(ctx, p.sessionID)
	if err != nil {
		return message.Message{}, err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return message.Message{}, errProjectionOperation
}

func (p *projectedMessages) List(_ context.Context, sessionID string) ([]message.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sessionID != p.sessionID {
		return nil, errProjectionOperation
	}
	return slices.Clone(p.messages), nil
}

func (p *projectedMessages) ListFromSummary(ctx context.Context, sessionID, summaryID string) ([]message.Message, error) {
	entries, err := p.List(ctx, sessionID)
	if err != nil || summaryID == "" {
		return entries, err
	}
	for index, entry := range entries {
		if entry.ID == summaryID {
			return entries[index:], nil
		}
	}
	return nil, errProjectionOperation
}

func (p *projectedMessages) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	entries, err := p.List(ctx, sessionID)
	return slices.DeleteFunc(entries, func(entry message.Message) bool { return entry.Role != message.User }), err
}

func (p *projectedMessages) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return p.ListUserMessages(ctx, p.sessionID)
}

func (p *projectedMessages) GetLastAssistantMessage(ctx context.Context, sessionID string) (message.Message, error) {
	entries, err := p.List(ctx, sessionID)
	if err != nil {
		return message.Message{}, err
	}
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].Role == message.Assistant {
			return entries[index], nil
		}
	}
	return message.Message{}, errProjectionOperation
}

func (p *projectedMessages) Delete(context.Context, string) error { return errProjectionOperation }
func (p *projectedMessages) DeleteSessionMessages(context.Context, string) error {
	return errProjectionOperation
}
func (p *projectedMessages) Flush(context.Context, string) error { return nil }
func (p *projectedMessages) FlushAll(context.Context) error      { return nil }

func (p *projectedSessions) Get(_ context.Context, id string) (session.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != p.current.ID {
		return session.Session{}, errProjectionOperation
	}
	return p.current, nil
}

func (p *projectedSessions) Save(_ context.Context, updated session.Session) (session.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if updated.ID != p.current.ID {
		return session.Session{}, errProjectionOperation
	}
	p.current = updated
	return updated, nil
}

func (p *projectedSessions) GetLast(context.Context) (session.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current, nil
}

func (p *projectedSessions) List(ctx context.Context) ([]session.Session, error) {
	entry, err := p.GetLast(ctx)
	return []session.Session{entry}, err
}

func (p *projectedSessions) Create(context.Context, string) (session.Session, error) {
	return session.Session{}, errProjectionOperation
}

func (p *projectedSessions) CreateTitleSession(context.Context, string) (session.Session, error) {
	return session.Session{}, errProjectionOperation
}

func (p *projectedSessions) CreateTaskSession(context.Context, string, string, string) (session.Session, error) {
	return session.Session{}, errProjectionOperation
}
func (p *projectedSessions) Delete(context.Context, string) error { return errProjectionOperation }
func (p *projectedSessions) Rename(context.Context, string, string) error {
	return errProjectionOperation
}

func (p *projectedSessions) UpdateTitleAndUsage(context.Context, string, string, int64, int64, float64) error {
	return errProjectionOperation
}

func (p *projectedSessions) SetChannel(context.Context, string, string) (session.Session, error) {
	return session.Session{}, errProjectionOperation
}
func (p *projectedSessions) MCPDisabledServers(context.Context) ([]string, error) { return nil, nil }
func (p *projectedSessions) MCPServersEnabled(context.Context) ([]string, error)  { return nil, nil }
func (p *projectedSessions) SetMCPServerDisabled(context.Context, string, bool) error {
	return errProjectionOperation
}

func (p *projectedSessions) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return messageID + "$$" + toolCallID
}

func (p *projectedSessions) ParseAgentToolSessionID(id string) (string, string, bool) {
	parts := strings.Split(id, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (p *projectedSessions) IsAgentToolSession(id string) bool {
	_, _, found := p.ParseAgentToolSessionID(id)
	return found
}
