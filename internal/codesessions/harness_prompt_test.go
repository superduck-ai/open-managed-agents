package codesessions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestHarnessModelConfigurationRejectsMalformedSnapshots(t *testing.T) {
	for _, claim := range []db.HarnessTurnClaim{
		{CodeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"append_system_prompt":42}}`)}},
		{CodeSession: db.CodeSession{Metadata: json.RawMessage(`{}`)}, Subagent: &db.SessionThread{AgentSnapshot: json.RawMessage(`{"name":""}`)}},
	} {
		if model, prompt, err := HarnessModelConfiguration(claim); err == nil || model != "" || prompt != "" {
			t.Fatalf("invalid configuration returned model %q, prompt %q, error %v", model, prompt, err)
		}
	}
}

func TestHarnessModelConfigurationLayers(t *testing.T) {
	code := db.CodeSession{Model: "main-model", WorkDir: "/workspace", Metadata: json.RawMessage(`{
		"config":{
			"system_prompt":"  Root {{agent}} instructions.",
			"custom_system_prompt":"Legacy fallback.",
			"append_system_prompt":"# Managed-agent environment\n\nOutbound network access is limited.",
			"memory_catalog_prompt":"\n\n<attached_memory_stores/>",
			"runtime_skills":[{"name":"pdf","path":"/root/.claude/skills/pdf/SKILL.md"}],
			"subagents":{"inlineAgents":false}
		}}`)}
	for _, tt := range []struct {
		name      string
		child     *db.SessionThread
		model     string
		platform  string
		role      string
		omissions []string
	}{
		{
			name: "root", model: "main-model", platform: "# Platform rules",
			role:      "# Agent instructions\n\n  Root {{agent}} instructions.",
			omissions: []string{"# Subagent rules", "Subagents are disabled for this session."},
		},
		{
			name:  "specialist",
			child: &db.SessionThread{AgentSnapshot: json.RawMessage(`{"name":"researcher","model":{"id":"child-model"},"system":"Inspect files."}`)},
			model: "child-model", platform: "# Subagent rules\n\nYou are subagent researcher.",
			role:      "# Specialist instructions\n\nInspect files.",
			omissions: []string{"# Agent instructions", "Root {{agent}} instructions.", "## Delegating to subagents", "AskUserQuestion"},
		},
		{
			name:  "inline",
			child: &db.SessionThread{AgentSnapshot: json.RawMessage(`{"name":"inspect","system":"  "}`)},
			model: "main-model", platform: "# Subagent rules\n\nYou are subagent inspect.",
			omissions: []string{"# Agent instructions", "# Specialist instructions", "## Delegating to subagents"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			claim := db.HarnessTurnClaim{CodeSession: code, Subagent: tt.child}
			model, prompt, err := HarnessModelConfiguration(claim)
			if err != nil || model != tt.model {
				t.Fatalf("got model %q, want %q, error %v", model, tt.model, err)
			}
			if !strings.HasPrefix(prompt, tt.platform) || strings.Contains(prompt, "\n\n\n") {
				t.Fatalf("unexpected prompt section spacing: %q", prompt)
			}
			remaining := prompt
			for _, section := range []string{tt.platform, "## Working", "## Approvals", "## Instructions and trust",
				"# Managed-agent environment\n\nOutbound network access is limited.", tt.role, "# This session\n\nWorking directory: /workspace", "<attached_memory_stores/>"} {
				_, after, found := strings.Cut(remaining, section)
				if !found {
					t.Fatalf("missing or out of order: %q", section)
				}
				remaining = after
			}
			for _, omitted := range append(tt.omissions, "Legacy fallback.", "/root/.claude/skills/", "<system-reminder>") {
				if strings.Contains(prompt, omitted) {
					t.Fatalf("unexpected prompt text %q", omitted)
				}
			}
			if tt.child == nil && (!strings.Contains(prompt, "## Delegating to subagents") || !strings.Contains(prompt, "You cannot define inline agents.")) {
				t.Fatal("root prompt lacks predefined-only delegation guidance")
			}
		})
	}
}

func TestHarnessModelConfigurationWithLegacyInstructionsAndDelegationDisabled(t *testing.T) {
	claim := db.HarnessTurnClaim{CodeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"custom_system_prompt":"Legacy instructions"}}`)}}
	_, prompt, err := HarnessModelConfiguration(claim)
	if err != nil || !strings.Contains(prompt, "# Agent instructions\n\nLegacy instructions") ||
		!strings.Contains(prompt, "Subagents are disabled for this session.") || strings.Contains(prompt, "## Delegating to subagents") {
		t.Fatalf("unexpected legacy session prompt: %q, %v", prompt, err)
	}
}
