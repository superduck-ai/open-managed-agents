//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestRuntimeResolverResolvesCustomLatestVersion(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore("skills-runtime-resolver-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()

	created := createSkill(t, app, "runtime-latest-skill")
	defer deleteSkill(t, app, created.ID)

	versions := listSkillVersions(t, app, created.ID, "")
	if len(versions.Data) != 1 {
		t.Fatalf("initial versions = %+v, want one", versions)
	}
	firstVersion := versions.Data[0]

	time.Sleep(time.Millisecond)
	body, contentType := skillMultipartBody(t, "", []skillUploadFile{
		{FieldName: "files[]", Filename: "runtime-latest-skill/SKILL.md", Content: "---\nname: Runtime Latest Skill v2\ndescription: v2 description\n---\n\n# Runtime Latest Skill v2"},
	})
	resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills/"+created.ID+"/versions?beta=true", body, defaultTestKey, true, contentType)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create second version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var secondVersion skillVersionAPIResponse
	decodeJSON(t, resp.Body, &secondVersion)

	resolver := skillsapi.NewRuntimeResolver(app.db)
	ids := getDefaultDBIDs(t, app.pool)
	snapshot, err := json.Marshal(map[string]any{
		"skills": []map[string]string{{
			"type":     "custom",
			"skill_id": created.ID,
			"version":  "latest",
		}},
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	resolved, err := resolver.ResolveAgentSnapshot(ctx, ids.WorkspaceUUID, snapshot)
	if err != nil {
		t.Fatalf("resolve latest custom skill: %v", err)
	}
	if len(resolved) != 1 || resolved[0].RequestedVersion != "latest" || resolved[0].Version != secondVersion.Version {
		t.Fatalf("resolved latest = %+v, want version %s", resolved, secondVersion.Version)
	}

	for _, versions := range [][2]string{{secondVersion.Version, "latest"}, {"latest", secondVersion.Version}} {
		aliases, err := jsonv2.Marshal(map[string]any{
			"skills": []map[string]string{{"type": "custom", "skill_id": created.ID, "version": versions[0]}},
			"multiagent": map[string]any{"agents": []map[string]any{
				{"skills": []map[string]string{{"type": "custom", "skill_id": created.ID, "version": versions[1]}}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		aliasesResolved, err := resolver.ResolveAgentSnapshot(t.Context(), ids.WorkspaceUUID, aliases)
		if err != nil || len(aliasesResolved) != 1 || aliasesResolved[0].RequestedVersion != "latest" || aliasesResolved[0].Version != secondVersion.Version {
			t.Fatalf("dedup aliases=%+v, error=%v", aliasesResolved, err)
		}
	}
	conflict := json.RawMessage(`{"skills":[{"type":"custom","skill_id":` + quoteJSON(created.ID) + `,"version":` + quoteJSON(firstVersion.Version) + `}],"multiagent":{"agents":[{"skills":[{"type":"custom","skill_id":` + quoteJSON(created.ID) + `,"version":` + quoteJSON(secondVersion.Version) + `}]}]}}`)
	if _, err := resolver.ResolveAgentSnapshot(t.Context(), ids.WorkspaceUUID, conflict); err == nil || !strings.Contains(err.Error(), "used by multiple skills") {
		t.Fatalf("parent and child skill directory conflict was not rejected: %v", err)
	}

	resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+created.ID+"/versions/"+secondVersion.Version+"?beta=true", nil, defaultTestKey, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete second version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	resolved, err = resolver.ResolveAgentSnapshot(ctx, ids.WorkspaceUUID, snapshot)
	if err != nil {
		t.Fatalf("resolve latest custom skill after delete: %v", err)
	}
	if len(resolved) != 1 || resolved[0].RequestedVersion != "latest" || resolved[0].Version != firstVersion.Version {
		t.Fatalf("resolved latest after delete = %+v, want version %s", resolved, firstVersion.Version)
	}
}
