package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

type validationCatalog struct {
	versions map[string]db.SkillVersion
	latest   db.SkillVersion
	failure  error
}

func (c *validationCatalog) GetBuiltinSkillVersion(context.Context, string, string) (db.BuiltinSkillVersion, error) {
	if c.failure != nil {
		return db.BuiltinSkillVersion{}, c.failure
	}
	return db.BuiltinSkillVersion{}, db.ErrNotFound
}
func (c *validationCatalog) GetLatestSkillVersion(_ context.Context, workspace, skill string) (db.SkillVersion, error) {
	if c.failure != nil {
		return db.SkillVersion{}, c.failure
	}
	if workspace != "workspace" || skill != "skill_a" {
		return db.SkillVersion{}, db.ErrNotFound
	}
	return c.latest, nil
}
func (c *validationCatalog) GetSkillVersion(_ context.Context, workspace, skill, version string) (db.SkillVersion, error) {
	if c.failure != nil {
		return db.SkillVersion{}, c.failure
	}
	value, ok := c.versions[workspace+"/"+skill+"/"+version]
	if !ok {
		return db.SkillVersion{}, db.ErrNotFound
	}
	return value, nil
}

func TestValidateSkillReferencesUsesExactWorkspaceSkillAndVersion(t *testing.T) {
	catalog := &validationCatalog{versions: map[string]db.SkillVersion{
		"workspace/skill_a/version_a": {Version: "version_a", Directory: "same"},
		"workspace/skill_b/version_b": {Version: "version_b", Directory: "same"},
	}}
	resolver := &RuntimeResolver{db: catalog}
	for _, tc := range []struct {
		workspace, snapshot string
		invalid             bool
	}{
		{"workspace", `{"skills":[{"type":"custom","skill_id":"skill_a","version":"version_a"}]}`, false},
		{"workspace", `{"skills":[{"type":"custom","skill_id":"skill_a","version":"version_b"}]}`, true},
		{"foreign", `{"skills":[{"type":"custom","skill_id":"skill_a","version":"version_a"}]}`, true},
		{"workspace", `{"skills":[{"type":"custom","skill_id":"display-label","version":"version_a"}]}`, true},
		{"workspace", `{"skills":[{"type":"anthropic","skill_id":"missing"}]}`, true},
		{"workspace", `{"skills":[{"type":"custom","skill_id":"skill_a","version":"version_a"}],"multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"skills":[{"type":"custom","skill_id":"skill_b","version":"version_b"}]}]}}}`, false},
	} {
		err := resolver.ValidateAgentSnapshot(context.Background(), tc.workspace, json.RawMessage(tc.snapshot))
		if errors.Is(err, ErrInvalidSkillReference) != tc.invalid || !tc.invalid && err != nil {
			t.Fatalf("validation %s: %v", tc.snapshot, err)
		}
	}
	storageFailure := errors.New("catalog unavailable")
	catalog.failure = storageFailure
	err := resolver.ValidateAgentSnapshot(context.Background(), "workspace", json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_a"}]}`))
	if !errors.Is(err, storageFailure) || errors.Is(err, ErrInvalidSkillReference) {
		t.Fatal("catalog failure mislabeled as invalid input", err)
	}
}

func TestReferenceValidationDoesNotPinLatestOrMutateSnapshot(t *testing.T) {
	catalog := &validationCatalog{latest: db.SkillVersion{Version: "version_a", Directory: "a"}}
	resolver := &RuntimeResolver{db: catalog}
	snapshot := json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_a","version":"latest"}]}`)
	before := bytes.Clone(snapshot)
	if err := resolver.ValidateAgentSnapshot(context.Background(), "workspace", snapshot); err != nil {
		t.Fatal(err)
	}
	catalog.latest = db.SkillVersion{Version: "version_b", Directory: "a"}
	resolved, err := resolver.ResolveAgentSnapshot(context.Background(), "workspace", snapshot)
	if err != nil || len(resolved) != 1 || resolved[0].Version != "version_b" || !bytes.Equal(before, snapshot) {
		t.Fatal("validation changed activation-time latest semantics", resolved, err)
	}
	if err := NewRuntimeResolver(nil).ValidateAgentSnapshot(context.Background(), "workspace", json.RawMessage(`{"skills":[]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentAgentVersionsReceiveDistinctImmutableDirectories(t *testing.T) {
	catalog := &validationCatalog{versions: map[string]db.SkillVersion{
		"workspace/skill_a/v1": {UUID: "11111111-1111-4111-8111-111111111111", Version: "v1", Directory: "same", SHA256: "old-source"},
	}, latest: db.SkillVersion{UUID: "22222222-2222-4222-8222-222222222222", Version: "v2", Directory: "same", SHA256: "new-source"}}
	resolver := &RuntimeResolver{db: catalog}
	snapshot := json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_a","version":"latest"}],"multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"skills":[{"type":"custom","skill_id":"skill_a","version":"v1"}]}]}}}`)
	resolved, err := resolver.ResolveAgentSnapshot(context.Background(), "workspace", snapshot)
	if err != nil || len(resolved) != 2 {
		t.Fatal(resolved, err)
	}
	if resolved[0].Directory == resolved[1].Directory || resolved[0].SHA256 != "new-source" || resolved[1].SHA256 != "old-source" {
		t.Fatal(resolved)
	}
	for _, skill := range resolved {
		if skill.ArchiveDirectory != "same" || skill.Directory != "same--"+skill.VersionUUID {
			t.Fatal(skill)
		}
	}
	bad := json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_a","version":"latest"},{"type":"custom","skill_id":"skill_a","version":"v1"}]}`)
	if _, err := resolver.ResolveAgentSnapshot(context.Background(), "workspace", bad); err == nil {
		t.Fatal("same-Agent directory collision accepted")
	}
	duplicate := json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_a","version":"v1"},{"type":"custom","skill_id":"skill_a","version":"v1"}]}`)
	single, err := resolver.ResolveAgentSnapshot(context.Background(), "workspace", duplicate)
	if err != nil || len(single) != 1 || single[0].Directory != "same" || single[0].ArchiveDirectory != "" {
		t.Fatal(single, err)
	}
}
