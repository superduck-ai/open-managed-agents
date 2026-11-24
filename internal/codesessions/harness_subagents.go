package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxtools"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type harnessDelegationArgs struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	Preset       string `json:"agent_preset"`
	AgentID      string `json:"agent_id"`
	Message      string `json:"message"`
	FreshContext *bool  `json:"fresh_context"`
}

type harnessDelegationSink interface {
	PublishHarnessDelegation(context.Context, db.CodeSession, db.HarnessTurnLease, db.HarnessTool, db.HarnessDelegation) error
}

func isHarnessDelegationTool(name string) bool {
	return name == "subagent" || name == "send_message" || name == "list_agents"
}

func harnessSubagentConfig(code db.CodeSession) (*RuntimeSubagentConfig, error) {
	var metadata struct {
		Config struct {
			Subagents *RuntimeSubagentConfig `json:"subagents"`
		} `json:"config"`
	}
	if len(code.Metadata) > 0 {
		if err := json.Unmarshal(code.Metadata, &metadata); err != nil {
			return nil, err
		}
	}
	return metadata.Config.Subagents, nil
}

func HarnessExecutionTools(session db.Session, claim db.HarnessTurnClaim) ([]sandboxtools.Definition, error) {
	if claim.Subagent != nil {
		parentTools, err := HarnessSandboxTools(session)
		if err != nil {
			return nil, err
		}
		session.AgentSnapshot = claim.Subagent.AgentSnapshot
		childTools, err := HarnessSandboxTools(session)
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(childTools, func(tool sandboxtools.Definition) bool {
			return !slices.ContainsFunc(parentTools, func(parent sandboxtools.Definition) bool { return parent.Name == tool.Name })
		}), nil
	}
	tools, err := HarnessModelTools(session)
	if err != nil {
		return nil, err
	}
	config, err := harnessSubagentConfig(claim.CodeSession)
	if err != nil || config == nil {
		return tools, err
	}
	return append(tools, harnessDelegationTools(config)...), nil
}

func harnessDelegationTools(config *RuntimeSubagentConfig) []sandboxtools.Definition {
	description := "Delegate a task and receive the subagent's report. Include the task, relevant context, and all applicable limits in prompt. Set fresh_context to false to inherit the parent history before this call."
	if !config.InlineAgents {
		description += " Select a configured agent_preset."
	}
	if len(config.Agents) > 0 {
		description += "\nAvailable presets:"
		for _, agent := range config.Agents {
			description += "\n- " + agent.Name + ": " + agent.Description
		}
	}
	return []sandboxtools.Definition{
		{Name: "subagent", Description: description, InputSchema: json.RawMessage(`{"type":"object","required":["description","prompt"],"properties":{"description":{"type":"string"},"prompt":{"type":"string"},"agent_preset":{"type":"string"},"fresh_context":{"type":"boolean","default":true}}}`)},
		{Name: "send_message", Description: "Give an existing subagent another task in its original thread and receive its report.", InputSchema: json.RawMessage(`{"type":"object","required":["agent_id","message"],"properties":{"agent_id":{"type":"string"},"message":{"type":"string"}}}`)},
		{Name: "list_agents", Description: "List the subagent threads in this session with their IDs and status.", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
}

func HarnessSubagentModel(thread db.SessionThread, parentModel string) (string, string, error) {
	var snapshot runtimeAgentSnapshot
	if err := json.Unmarshal(thread.AgentSnapshot, &snapshot); err != nil {
		return "", "", err
	}
	definition, err := runtimeSubagentDefinition(snapshot, builtinToolCatalog{})
	if err != nil {
		return "", "", err
	}
	model := definition.Model
	if model == "" {
		model = parentModel
	}
	prompt := "You are the subagent " + definition.Name + ". Complete the assigned task within its stated constraints. Report your findings, results, and blockers to the parent agent. Ask the parent for any needed information."
	if definition.SystemPrompt != "" {
		prompt += "\n\n" + definition.SystemPrompt
	}
	return model, prompt, nil
}

func (h *Handler) harnessDelegationOutcome(ctx context.Context, code db.CodeSession, lease db.HarnessTurnLease, tool db.HarnessTool) (sessioncontract.HarnessToolResult, bool, error) {
	config, err := harnessSubagentConfig(code)
	if err != nil {
		return sessioncontract.HarnessToolResult{}, false, err
	}
	if config == nil || tool.DelegationToolUUID != nil {
		return failedHarnessDelegation("Subagent delegation is disabled for this thread."), false, nil
	}
	if tool.Name == "list_agents" {
		result, err := h.harnessListSubagents(ctx, code)
		return result, false, err
	}
	var args harnessDelegationArgs
	if err := json.Unmarshal(tool.Arguments, &args); err != nil {
		return failedHarnessDelegation("Invalid subagent arguments."), false, nil
	}
	delegation, failure, err := h.buildHarnessDelegation(ctx, code, config, tool.Name, args)
	if err != nil || failure != "" {
		return failedHarnessDelegation(failure), false, err
	}
	sink, ok := h.service.sink.(harnessDelegationSink)
	if !ok {
		return sessioncontract.HarnessToolResult{}, false, db.ErrInvalidState
	}
	err = sink.PublishHarnessDelegation(ctx, code, lease, tool, delegation)
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrSessionInputConflict) {
		return failedHarnessDelegation("The subagent thread is unavailable."), false, nil
	}
	return sessioncontract.HarnessToolResult{}, err == nil, err
}

func (h *Handler) buildHarnessDelegation(ctx context.Context, code db.CodeSession, config *RuntimeSubagentConfig, name string, args harnessDelegationArgs) (db.HarnessDelegation, string, error) {
	if name == "send_message" {
		id, prompt := strings.TrimSpace(args.AgentID), strings.TrimSpace(args.Message)
		if id == "" || !validHarnessDelegationPrompt(prompt) {
			return db.HarnessDelegation{}, "Provide a subagent ID and a nonempty message of at most 16 KiB.", nil
		}
		return db.HarnessDelegation{ThreadID: id, Prompt: prompt}, "", nil
	}
	description, prompt := strings.TrimSpace(args.Description), strings.TrimSpace(args.Prompt)
	if description == "" || len(description) > 256 || !validHarnessDelegationPrompt(prompt) {
		return db.HarnessDelegation{}, "Provide a short description and a nonempty task of at most 16 KiB.", nil
	}
	inherit := args.FreshContext != nil && !*args.FreshContext
	if preset := strings.TrimSpace(args.Preset); preset != "" {
		for _, agent := range config.Agents {
			if agent.Name == preset {
				return db.HarnessDelegation{AgentSnapshot: agent.AgentSnapshot, Prompt: prompt, InheritContext: inherit}, "", nil
			}
		}
		return db.HarnessDelegation{}, "Unknown subagent preset.", nil
	}
	if !config.InlineAgents {
		return db.HarnessDelegation{}, "Choose a configured agent_preset.", nil
	}
	session, found, err := h.db.GetSessionByUUID(ctx, code.WorkspaceUUID, code.SessionUUID)
	if err != nil {
		return db.HarnessDelegation{}, "", err
	}
	if !found || session.OrganizationUUID != code.OrganizationUUID {
		return db.HarnessDelegation{}, "", db.ErrNotFound
	}
	snapshot, err := harnessInlineSnapshot(session.AgentSnapshot, description)
	return db.HarnessDelegation{AgentSnapshot: snapshot, Prompt: prompt, InheritContext: inherit}, "", err
}

func harnessInlineSnapshot(parent json.RawMessage, name string) (json.RawMessage, error) {
	var snapshot struct {
		Type   string          `json:"type"`
		Name   string          `json:"name"`
		Model  json.RawMessage `json:"model"`
		Tools  json.RawMessage `json:"tools"`
		System string          `json:"system"`
	}
	if err := json.Unmarshal(parent, &snapshot); err != nil {
		return nil, err
	}
	snapshot.Type, snapshot.Name, snapshot.System = "inline", name, ""
	return json.Marshal(snapshot)
}

func validHarnessDelegationPrompt(text string) bool {
	return text != "" && len(text) <= sessioncontract.HarnessInputMaxBytes && !strings.ContainsRune(text, 0)
}

func failedHarnessDelegation(text string) sessioncontract.HarnessToolResult {
	return sessioncontract.HarnessToolResult{Text: text, Failed: true}
}

func (h *Handler) harnessListSubagents(ctx context.Context, code db.CodeSession) (sessioncontract.HarnessToolResult, error) {
	threads, err := h.db.ListSessionThreads(ctx, code.WorkspaceUUID, code.SessionExternalID)
	if err != nil {
		return sessioncontract.HarnessToolResult{}, err
	}
	type entry struct {
		ID     string `json:"agent_id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	result := make([]entry, 0)
	for _, thread := range threads {
		if thread.ParentThreadUUID == nil || thread.ArchivedAt != nil {
			continue
		}
		var agent struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(thread.AgentSnapshot, &agent); err != nil {
			return sessioncontract.HarnessToolResult{}, err
		}
		result = append(result, entry{ID: thread.ExternalID, Name: agent.Name, Status: thread.Status})
	}
	raw, err := json.Marshal(result)
	if len(raw) > sessioncontract.HarnessToolResultMaxBytes {
		return failedHarnessDelegation("The subagent list is too large to return."), nil
	}
	return sessioncontract.HarnessToolResult{Text: string(raw)}, err
}
