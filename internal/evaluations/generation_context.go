package evaluations

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	maxNoteLength        = 2000
	maxSeedCases         = 10
	maxAgentSystemRunes  = 8000
	maxToolDescription   = 300
	maxSeedQueryRunes    = 300
	maxSeedAnswerRunes   = 200
	builtinToolsetType   = "agent_toolset_20260401"
	mcpToolsetType       = "mcp_toolset"
	customToolType       = "custom"
	generationListMarker = "- "
)

var builtinToolNames = []string{"bash", "read", "write", "edit", "glob", "grep", "web_fetch", "web_search", "ask_user_question"}

type generationSource struct {
	AgentID      string `json:"agent_id"`
	AgentVersion int    `json:"agent_version"`
	DatasetID    string `json:"dataset_id"`
	Note         string `json:"note"`
}

type generationContext struct {
	agent    string
	seeds    []db.EvaluationCase
	existing []string
	note     string
}

type agentToolEntry struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	MCPServerName string `json:"mcp_server_name"`
	DefaultConfig struct {
		Enabled *bool `json:"enabled"`
	} `json:"default_config"`
	Configs []struct {
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
	} `json:"configs"`
}

func (source generationSource) normalized() (generationSource, error) {
	source.AgentID = strings.TrimSpace(source.AgentID)
	source.DatasetID = strings.TrimSpace(source.DatasetID)
	source.Note = strings.TrimSpace(source.Note)
	if utf8.RuneCountInString(source.Note) > maxNoteLength {
		return generationSource{}, invalidArgument(noteTooLongMessage, nil)
	}
	if source.AgentID == "" && source.DatasetID == "" && source.Note == "" {
		return generationSource{}, invalidArgument(generateSourceMessage, nil)
	}
	return source, nil
}

func (h *Handler) loadGenerationContext(ctx context.Context, principal auth.Principal, source generationSource) (generationContext, error) {
	loaded := generationContext{note: source.Note}
	if source.AgentID != "" {
		agent, err := h.agentForGeneration(ctx, principal.WorkspaceUUID, source.AgentID, source.AgentVersion)
		if err != nil {
			return generationContext{}, err
		}
		loaded.agent = agentBrief(agent)
	}
	if source.DatasetID != "" {
		cases, err := h.store.ListEvaluationCases(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, source.DatasetID)
		if err != nil {
			return generationContext{}, mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
		}
		loaded.seeds = spreadCases(cases, maxSeedCases)
		loaded.existing = make([]string, 0, len(cases))
		for _, item := range cases {
			loaded.existing = append(loaded.existing, item.Query)
		}
	}
	return loaded, nil
}

func (loaded generationContext) write(builder *strings.Builder) {
	if loaded.agent != "" {
		writeTagged(builder, "agent", loaded.agent)
	}
	if len(loaded.seeds) > 0 {
		writeTagged(builder, "existing_cases", seedCaseLines(loaded.seeds))
	}
	if loaded.note != "" {
		writeTagged(builder, "note", loaded.note)
	}
}

func spreadCases(cases []db.EvaluationCase, limit int) []db.EvaluationCase {
	if len(cases) <= limit {
		return cases
	}
	picked := make([]db.EvaluationCase, 0, limit)
	for index := range limit {
		picked = append(picked, cases[index*len(cases)/limit])
	}
	return picked
}

func seedCaseLines(cases []db.EvaluationCase) string {
	lines := make([]string, 0, len(cases))
	for _, item := range cases {
		line := generationListMarker + clipRunes(item.Query, maxSeedQueryRunes)
		if item.ExpectedAnswer != "" {
			line += "（期望：" + clipRunes(item.ExpectedAnswer, maxSeedAnswerRunes) + "）"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func agentBrief(agent db.Agent) string {
	var builder strings.Builder
	builder.WriteString("名称：" + agent.Name + "\n")
	if agent.Description != nil && *agent.Description != "" {
		builder.WriteString("说明：" + *agent.Description + "\n")
	}
	if agent.System != nil && *agent.System != "" {
		builder.WriteString("系统提示词：\n" + clipRunes(*agent.System, maxAgentSystemRunes) + "\n")
	}
	tools := agentToolLines(agent.Tools)
	if len(tools) == 0 {
		builder.WriteString("可用工具：无\n")
	} else {
		builder.WriteString("可用工具：\n" + strings.Join(tools, "\n") + "\n")
	}
	if skills := agentSkillIDs(agent.Skills); len(skills) > 0 {
		builder.WriteString("技能：" + strings.Join(skills, "、") + "\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func agentToolLines(raw json.RawMessage) []string {
	var entries []agentToolEntry
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var lines []string
	for _, entry := range entries {
		switch entry.Type {
		case builtinToolsetType:
			lines = append(lines, enabledBuiltinTools(entry)...)
		case mcpToolsetType:
			lines = append(lines, mcpToolLine(entry))
		case customToolType:
			lines = append(lines, generationListMarker+entry.Name+"："+clipRunes(entry.Description, maxToolDescription))
		}
	}
	return lines
}

func enabledBuiltinTools(entry agentToolEntry) []string {
	enabled := entry.DefaultConfig.Enabled == nil || *entry.DefaultConfig.Enabled
	overrides := map[string]bool{}
	for _, config := range entry.Configs {
		if config.Enabled != nil {
			overrides[config.Name] = *config.Enabled
		}
	}
	var lines []string
	for _, name := range builtinToolNames {
		on, ok := overrides[name]
		if !ok {
			on = enabled
		}
		if on {
			lines = append(lines, generationListMarker+name)
		}
	}
	return lines
}

func mcpToolLine(entry agentToolEntry) string {
	line := generationListMarker + "MCP 服务 " + entry.MCPServerName
	var named []string
	for _, config := range entry.Configs {
		if config.Enabled == nil || *config.Enabled {
			named = append(named, config.Name)
		}
	}
	if len(named) > 0 {
		line += "：" + strings.Join(named, "、")
	}
	return line
}

func agentSkillIDs(raw json.RawMessage) []string {
	var entries []struct {
		SkillID string `json:"skill_id"`
	}
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.SkillID != "" {
			ids = append(ids, entry.SkillID)
		}
	}
	return ids
}
