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
		{name: "invalid child type", snapshot: `{"multiagent":{"agents":[{"skills":[{"type":"unknown","skill_id":"review"}]}]}}`, wantErr: true},
		{name: "missing child skill ID", snapshot: `{"multiagent":{"agents":[{"skills":[{"type":"custom"}]}]}}`, wantErr: true},
		{name: "missing skills", snapshot: `{}`},
		{name: "null skills", snapshot: `{"skills":null}`},
		{name: "null snapshot", snapshot: `null`},
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
