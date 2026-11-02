package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"strings"
	"testing"
)

func TestSelectRuntimeSkillsRejectsMissingFixedDirectory(t *testing.T) {
	_, err := selectRuntimeSkills(nil, nil, json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_a"}]}`), false)
	if err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestSelectRuntimeSkillsRejectsInvalidSnapshot(t *testing.T) {
	if _, err := selectRuntimeSkills(nil, nil, json.RawMessage(`{"skills":42}`), false); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
}

func TestSelectRuntimeSkillsKeepsAgentScope(t *testing.T) {
	skills := []runtimeSkillConfig{{Name: "parent", Path: "/root/.claude/skills/parent/SKILL.md"}, {Name: "child", Path: "/root/.claude/skills/child/SKILL.md"}}
	directories := map[string]string{"custom:skill_parent:latest": "parent", "custom:skill_child:v1": "child"}
	selected, err := selectRuntimeSkills(skills, directories, json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_child","version":"v1"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "child" {
		t.Fatalf("skills = %+v", selected)
	}
	empty, err := selectRuntimeSkills(skills, directories, json.RawMessage(`{"skills":[]}`), false)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty scope = %+v, %v", empty, err)
	}
}

func TestSkillFrontmatterOmitsBody(t *testing.T) {
	for _, document := range []string{"---\nname: pdf\n---\nprivate body", "---\r\nname: pdf\r\n---\r\nprivate body"} {
		if got, err := skillFrontmatter(document); err != nil || got != "---\nname: pdf\n---\n" {
			t.Fatalf("frontmatter = %q", got)
		}
	}
	if got, err := skillFrontmatter("instructions"); err != nil || got != "" {
		t.Fatalf("frontmatter = %q", got)
	}
}

type skillReaderFixture struct{ calls []string }

func (f *skillReaderFixture) ReadSessionSkill(_ context.Context, workspaceUUID, sessionID, directory string) ([]byte, error) {
	f.calls = append(f.calls, workspaceUUID+":"+sessionID+":"+directory)
	return []byte("---\nname: child\n---\nprivate body"), nil
}

func TestSessionSkillDocumentsRejectsUnselectedDirectory(t *testing.T) {
	reader := &skillReaderFixture{}
	service := &Service{skillReader: reader}
	record := skillDocumentTestRecord()
	_, err := service.sessionSkillDocuments(t.Context(), record, json.RawMessage(`{"skills":[]}`), "child", "thread")
	if !errors.Is(err, db.ErrNotFound) || len(reader.calls) != 0 {
		t.Fatalf("error = %v, reads = %v", err, reader.calls)
	}
}

func TestSessionSkillDocumentsLoadsPinnedScopeWithoutCatalogLookup(t *testing.T) {
	reader := &skillReaderFixture{}
	service := &Service{skillReader: reader}
	snapshot := json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_child"}]}`)
	documents, err := service.sessionSkillDocuments(t.Context(), skillDocumentTestRecord(), snapshot, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 || documents[0].Document != "---\nname: child\n---\n" {
		t.Fatalf("catalog = %+v", documents)
	}
	loaded, err := service.sessionSkillDocuments(t.Context(), skillDocumentTestRecord(), snapshot, "child", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || !strings.Contains(loaded[0].Document, "private body") {
		t.Fatalf("document = %+v", loaded)
	}
	if len(reader.calls) != 2 || reader.calls[0] != "workspace:session:child" {
		t.Fatalf("reads = %v", reader.calls)
	}
}

func skillDocumentTestRecord() db.CodeSession {
	return db.CodeSession{WorkspaceUUID: "workspace", SessionExternalID: "session", Metadata: json.RawMessage(`{"config":{"runtime_skills":[{"name":"child","path":"/root/.claude/skills/child/SKILL.md"}],"skill_directories":{"custom:skill_child:latest":"child"}}}`)}
}

func TestSelectRuntimeSkillsReusesLegacySingleAgentCatalog(t *testing.T) {
	skills := []runtimeSkillConfig{{Name: "pdf", Path: "/root/.claude/skills/pdf/SKILL.md"}}
	selected, err := selectRuntimeSkills(skills, nil, json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_old"}]}`), true)
	if err != nil || len(selected) != 1 {
		t.Fatalf("legacy skills = %+v, %v", selected, err)
	}
}

func TestSelectRuntimeSkillsRejectsLegacyChildWithoutMapping(t *testing.T) {
	skills := []runtimeSkillConfig{{Name: "pdf", Path: "/root/.claude/skills/pdf/SKILL.md"}}
	_, err := selectRuntimeSkills(skills, nil, json.RawMessage(`{"skills":[{"type":"custom","skill_id":"skill_other"}]}`), false)
	if err == nil {
		t.Fatal("child without fixed directory mapping accepted")
	}
}

func TestSkillFrontmatterRejectsUnterminatedHeader(t *testing.T) {
	if _, err := skillFrontmatter("---\nname: pdf\nprivate body"); !errors.Is(err, errRuntimeSkillFrontmatter) {
		t.Fatalf("error = %v", err)
	}
}
