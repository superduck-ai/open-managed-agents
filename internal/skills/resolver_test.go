package skills

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestRuntimeSkillRefsConfigurationBoundaries(t *testing.T) {
	child := runtimeSkillRef{Type: "custom", SkillID: "review", Version: "1"}
	for _, test := range []struct {
		name     string
		snapshot string
		want     []runtimeSkillRef
		wantErr  bool
	}{
		{name: "invalid snapshot", snapshot: `{`, wantErr: true},
		{name: "invalid skills", snapshot: `{"skills":{}}`, wantErr: true},
		{name: "invalid roster", snapshot: `{"multiagent":{"agents":{}}}`, wantErr: true},
		{name: "invalid child skills", snapshot: `{"multiagent":{"agents":[{"skills":{}}]}}`, wantErr: true},
		{name: "duplicate fields", snapshot: `{"skills":[],"skills":[]}`, wantErr: true},
		{name: "duplicate case aliases", snapshot: `{"skills":[],"SKILLS":[]}`, wantErr: true},
		{name: "invalid UTF8", snapshot: "{\"skills\":[],\"unknown\":\"\xff\"}", wantErr: true},
		{name: "skill ID delimiter differs", snapshot: `{"skills":[{"type":"custom","skillid":"review"}]}`, wantErr: true},
		{name: "invalid child type", snapshot: `{"multiagent":{"agents":[{"skills":[{"type":"unknown","skill_id":"review"}]}]}}`, wantErr: true},
		{name: "missing child skill ID", snapshot: `{"multiagent":{"agents":[{"skills":[{"type":"custom"}]}]}}`, wantErr: true},
		{name: "invalid CMA child skills", snapshot: `{"multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"skills":[{"type":"unknown"}]}]}}}`, wantErr: true},
		{name: "CMA independent lists", snapshot: `{"multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"skills":[{"type":"custom","skill_id":"review","version":"1"}]}]},"workflows":{"type":"enabled","predefined_agents":[{"skills":[{"type":"anthropic","skill_id":"workflow"}]}]}}}`, want: []runtimeSkillRef{child, {Type: "anthropic", SkillID: "workflow"}}},
		{name: "missing skills", snapshot: `{}`},
		{name: "null skills", snapshot: `{"skills":null}`},
		{name: "null snapshot", snapshot: `null`},
		{name: "empty skills", snapshot: `{"skills":[],"multiagent":{"agents":[{"skills":null},{}]}}`},
		{name: "case insensitive skill fields", snapshot: `{"SKILLS":[{"TYPE":"custom","SKILL_ID":"review","VERSION":"1"}]}`, want: []runtimeSkillRef{child}},
		{name: "case sensitive roster", snapshot: `{"Multiagent":{"agents":[{"skills":[{"type":"unknown"}]}]}}`},
		{name: "case sensitive child list", snapshot: `{"multiagent":{"Agents":[{"skills":[{"type":"unknown"}]}]}}`},
		{name: "child with missing parent skills", snapshot: `{"multiagent":{"agents":[{"skills":[{"type":"custom","skill_id":"review","version":"1"}]}]}}`, want: []runtimeSkillRef{child}},
		{name: "child with null parent skills", snapshot: `{"skills":null,"multiagent":{"agents":[{"skills":[{"type":"custom","skill_id":"review","version":"1"}]}]}}`, want: []runtimeSkillRef{child}},
		{name: "parent and child order", snapshot: `{"skills":[{"type":"anthropic","skill_id":"parent"}],"multiagent":{"agents":[{"skills":[{"type":"custom","skill_id":"review","version":"1"}]}]}}`, want: []runtimeSkillRef{{Type: "anthropic", SkillID: "parent"}, child}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := runtimeSkillRefs(json.RawMessage(test.snapshot))
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v, wantErr=%t", err, test.wantErr)
			}
			if err == nil && !slices.Equal(got, test.want) {
				t.Fatalf("skills=%+v, want=%+v", got, test.want)
			}
		})
	}
}
