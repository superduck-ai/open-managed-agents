package codesessions

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

//go:embed templates/harness_*.md.tpl
var harnessPromptSources embed.FS

var harnessPromptTemplates = template.Must(template.ParseFS(harnessPromptSources, "templates/harness_*.md.tpl"))

type harnessSystemPromptData struct {
	SubagentName      string
	DelegationEnabled bool
	InlineAgents      bool
	Environment       string
	Instructions      string
	WorkDir           string
	MemoryCatalog     string
}

func HarnessModelConfiguration(claim db.HarnessTurnClaim) (string, string, error) {
	var metadata struct {
		Config managedAgentRuntimeConfigSchema `json:"config"`
	}
	code := claim.CodeSession
	if err := json.Unmarshal(code.Metadata, &metadata); err != nil {
		return "", "", fmt.Errorf("decode harness session configuration: %w", err)
	}
	config := metadata.Config
	model, instructions := code.Model, cmp.Or(config.SystemPrompt, config.CustomSystemPrompt)
	data := harnessSystemPromptData{
		DelegationEnabled: config.Subagents != nil,
		InlineAgents:      config.Subagents != nil && config.Subagents.InlineAgents,
		Environment:       strings.TrimSpace(config.AppendSystemPrompt),
		WorkDir:           code.WorkDir,
		MemoryCatalog:     strings.TrimSpace(config.MemoryCatalogPrompt),
	}
	if claim.Subagent != nil {
		var snapshot runtimeAgentSnapshot
		if err := json.Unmarshal(claim.Subagent.AgentSnapshot, &snapshot); err != nil {
			return "", "", err
		}
		definition, err := runtimeSubagentDefinition(snapshot, builtinToolCatalog{})
		if err != nil {
			return "", "", err
		}
		model, instructions = cmp.Or(definition.Model, model), definition.SystemPrompt
		data.SubagentName = definition.Name
	}
	data.Instructions = strings.TrimRightFunc(instructions, unicode.IsSpace)
	prompt, err := renderHarnessPrompt("harness_system.md.tpl", data)
	if err != nil {
		return "", "", err
	}
	return model, prompt, nil
}

func HarnessTimeReminder(now time.Time) (string, error) {
	beijing := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	data := struct{ Timestamp string }{Timestamp: beijing.Format(time.RFC3339) + "[" + beijing.Location().String() + "]"}
	return renderHarnessPrompt("harness_time_reminder.md.tpl", data)
}

func renderHarnessPrompt(name string, data any) (string, error) {
	var prompt strings.Builder
	if err := harnessPromptTemplates.ExecuteTemplate(&prompt, name, data); err != nil {
		return "", fmt.Errorf("render harness prompt %s: %w", name, err)
	}
	return strings.TrimSuffix(prompt.String(), "\n"), nil
}
