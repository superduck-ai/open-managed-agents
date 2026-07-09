package tests

import (
	"context"
	"encoding/json"
	"net/http"
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

	resolver := skillsapi.NewRuntimeResolver(app.cfg, app.db, store)
	ids := getDefaultDBIDs(t, app.db)
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

	resolved, err := resolver.ResolveAgentSnapshot(ctx, ids.WorkspaceID, snapshot)
	if err != nil {
		t.Fatalf("resolve latest custom skill: %v", err)
	}
	if len(resolved) != 1 || resolved[0].RequestedVersion != "latest" || resolved[0].Version != secondVersion.Version {
		t.Fatalf("resolved latest = %+v, want version %s", resolved, secondVersion.Version)
	}

	resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+created.ID+"/versions/"+secondVersion.Version+"?beta=true", nil, defaultTestKey, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete second version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	resolved, err = resolver.ResolveAgentSnapshot(ctx, ids.WorkspaceID, snapshot)
	if err != nil {
		t.Fatalf("resolve latest custom skill after delete: %v", err)
	}
	if len(resolved) != 1 || resolved[0].RequestedVersion != "latest" || resolved[0].Version != firstVersion.Version {
		t.Fatalf("resolved latest after delete = %+v, want version %s", resolved, firstVersion.Version)
	}
}
