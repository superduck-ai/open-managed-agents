package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"charm.land/fantasy"
	crushruntime "github.com/charmbracelet/crush/runtime"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

type toolAssociation struct {
	publicID   string
	resultType string
}

type turn struct {
	execution  *execution
	history    *historyStore
	threadID   string
	tools      map[string]toolAssociation
	previewIDs []string
}

type deliveredInput struct {
	input codesessions.HostInput
	err   error
}

type completedTurn struct {
	turn *turn
	err  error
}

func (e *execution) run(ctx context.Context) {
	defer func() {
		e.cancel()
		e.children.Wait()
		e.closeConnections()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.worker.Close(cleanupCtx)
		e.service.mu.Lock()
		if e.service.active[e.codeSessionID] == e {
			delete(e.service.active, e.codeSessionID)
		}
		e.service.mu.Unlock()
		if e.done != nil {
			close(e.done)
		}
	}()
	if err := e.recover(ctx); err != nil {
		e.logFailure(ctx)
		return
	}
	inputs := make(chan deliveredInput)
	e.children.Add(1)
	go func() { defer e.children.Done(); e.receive(ctx, inputs) }()
	completed := make(chan completedTurn, 1)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if e.heartbeat(ctx) != nil {
				e.logFailure(ctx)
				return
			}
		case next := <-inputs:
			if next.err != nil {
				if ctx.Err() == nil {
					e.logFailure(ctx)
				}
				return
			}
			if err := e.handleInput(ctx, next.input, completed); err != nil {
				e.logFailure(ctx)
				return
			}
		case done := <-completed:
			e.turnCancel = nil
			if err := e.finish(ctx, done); err != nil {
				e.logFailure(ctx)
				return
			}
			if err := e.startPending(ctx, completed); err != nil {
				e.logFailure(ctx)
				return
			}
		}
	}
}

func (e *execution) receive(ctx context.Context, inputs chan<- deliveredInput) {
	for {
		input, err := e.worker.Next(ctx)
		select {
		case inputs <- deliveredInput{input: input, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func (e *execution) logFailure(ctx context.Context) {
	e.service.logger.ErrorContext(ctx, "host agent executor stopped", "code_session_id", e.codeSessionID, "worker_epoch", e.epoch, "error", ErrExecutionFailed)
}

func (e *execution) recover(ctx context.Context) error {
	history, err := e.worker.LoadHistory(ctx)
	if err != nil {
		return err
	}
	pending := interruptedRuns(history)
	for _, entry := range pending {
		store := &historyStore{worker: e.worker, codeSessionID: e.codeSessionID, runID: entry.RunID, inputEventID: entry.InputEventID, accepted: true}
		if err := resolveUndispatched(ctx, store, nil); err != nil {
			return err
		}
		if err := e.message(ctx, store.id("interrupted"), ErrPendingRun.Error()); err != nil {
			return err
		}
		if err := e.worker.AppendHistory(ctx, []codesessions.HostHistoryEntry{store.marker("run_finished", "finish")}); err != nil {
			return err
		}
	}
	return e.worker.EndTurn(ctx, len(pending) > 0)
}

func (e *execution) handleInput(ctx context.Context, input codesessions.HostInput, completed chan<- completedTurn) error {
	if err := e.worker.Acknowledge(ctx, input.EventID, "received"); err != nil {
		return err
	}
	var payload inputPayload
	if err := json.Unmarshal(input.Payload, &payload); err != nil {
		return ErrInvalidInput
	}
	switch payload.Type {
	case "control_response":
		e.permissionMu.Lock()
		waiting := e.permissions[payload.Response.RequestID]
		e.permissionMu.Unlock()
		if waiting != nil {
			select {
			case waiting <- payload.Response.Response:
			default:
			}
		}
		return e.worker.Acknowledge(ctx, input.EventID, "processed")
	case "control_request":
		if payload.Request.Subtype == "interrupt" && e.turnCancel != nil {
			e.turnCancel()
		}
		return e.worker.Acknowledge(ctx, input.EventID, "processed")
	case "user":
		return e.startTurn(ctx, input, payload, completed)
	default:
		return e.worker.Acknowledge(ctx, input.EventID, "processed")
	}
}

func (e *execution) startTurn(ctx context.Context, input codesessions.HostInput, payload inputPayload, completed chan<- completedTurn) error {
	runID := codesessions.HostRunID(e.codeSessionID, input.EventID)
	store := &historyStore{worker: e.worker, codeSessionID: e.codeSessionID, runID: runID, inputEventID: input.EventID}
	history, err := e.worker.LoadHistory(ctx)
	if err != nil {
		return err
	}
	for _, entry := range history {
		if entry.InputEventID == input.EventID && entry.Role == "run_started" {
			return e.worker.Acknowledge(ctx, input.EventID, "processed")
		}
	}
	if e.turnCancel != nil {
		for _, pending := range e.pending {
			if pending.EventID == input.EventID {
				return nil
			}
		}
		if len(e.pending) >= 32 {
			return e.reject(ctx, store, ErrInvalidInput)
		}
		e.pending = append(e.pending, input)
		return nil
	}
	prompt, files, err := decodePrompt(payload.Message.Content)
	if err != nil {
		return e.reject(ctx, store, ErrInvalidInput)
	}
	current := &turn{execution: e, history: store, threadID: payload.SessionThreadID, tools: make(map[string]toolAssociation)}
	var tools []fantasy.AgentTool
	for _, connection := range e.connections {
		discovered, err := connection.agentTools(current)
		if err != nil {
			return err
		}
		tools = append(tools, discovered...)
	}
	engine, err := crushruntime.New(crushruntime.Options{Model: e.model, SystemPrompt: e.configuration.SystemPrompt + "\n\n" + e.configuration.AppendSystemPrompt, History: store, Events: &eventSink{turn: current}, MaxSteps: 64})
	if err != nil {
		return err
	}
	if err := e.worker.SetState(ctx, "running"); err != nil {
		return err
	}
	turnCtx, cancel := context.WithCancel(ctx)
	e.turnCancel = cancel
	e.children.Add(1)
	go func() {
		defer e.children.Done()
		_, err := engine.Run(turnCtx, crushruntime.Call{SessionID: e.codeSessionID, RunID: runID, Prompt: prompt, Files: files, Tools: tools})
		cancel()
		select {
		case completed <- completedTurn{turn: current, err: err}:
		case <-ctx.Done():
		}
	}()
	return nil
}

func (e *execution) reject(ctx context.Context, history *historyStore, reason error) error {
	if err := e.worker.AppendHistory(ctx, []codesessions.HostHistoryEntry{history.marker("run_started", "start"), history.marker("run_finished", "finish")}); err != nil {
		return err
	}
	if err := e.message(ctx, history.id("rejected"), reason.Error()); err != nil {
		return err
	}
	return e.worker.Acknowledge(ctx, history.inputEventID, "processed")
}

func (e *execution) finish(ctx context.Context, done completedTurn) error {
	if done.err != nil {
		if err := resolveUndispatched(ctx, done.turn.history, done.turn); err != nil {
			return err
		}
		reason := ErrExecutionFailed
		if errors.Is(done.err, crushruntime.ErrPendingTools) {
			reason = ErrPendingRun
		}
		if errors.Is(done.err, context.Canceled) {
			reason = ErrInterrupted
		}
		if err := done.turn.publish(ctx, publicEvent{Type: "system.message", ID: done.turn.history.id("error"), Content: []publicContent{{Type: "text", Text: reason.Error()}}, EventIDs: done.turn.previewIDs}); err != nil {
			return err
		}
	}
	if done.turn.history.accepted {
		if err := e.worker.AppendHistory(ctx, []codesessions.HostHistoryEntry{done.turn.history.marker("run_finished", "finish")}); err != nil {
			return err
		}
	} else {
		if err := e.reject(ctx, done.turn.history, ErrExecutionFailed); err != nil {
			return err
		}
	}
	return e.worker.EndTurn(ctx, done.err != nil)
}

func (e *execution) message(ctx context.Context, id, text string) error {
	payload, err := json.Marshal(publicEvent{Type: "system.message", ID: id, Content: []publicContent{{Type: "text", Text: text}}})
	if err != nil {
		return err
	}
	return e.worker.Publish(ctx, payload)
}

func (e *execution) startPending(ctx context.Context, completed chan<- completedTurn) error {
	for e.turnCancel == nil && len(e.pending) > 0 {
		input := e.pending[0]
		e.pending = e.pending[1:]
		var payload inputPayload
		if err := json.Unmarshal(input.Payload, &payload); err != nil {
			return ErrInvalidInput
		}
		if err := e.startTurn(ctx, input, payload, completed); err != nil {
			return err
		}
	}
	return nil
}

func (e *execution) heartbeat(ctx context.Context) error {
	if err := e.worker.Heartbeat(ctx); err != nil {
		return err
	}
	for _, pending := range e.pending {
		if err := e.worker.Acknowledge(ctx, pending.EventID, "received"); err != nil {
			return err
		}
	}
	return nil
}
