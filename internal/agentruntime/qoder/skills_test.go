package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
)

func TestQoderSkillsLifecycleAgentTranslationAndIDOR(t *testing.T) {
	now := time.Date(2026, 11, 27, 11, 0, 0, 0, time.UTC)
	var upstreamHits atomic.Int32
	var lastUploadField string
	var lastAgentSkills []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/skills":
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			if files := r.MultipartForm.File["files"]; len(files) > 0 {
				lastUploadField = "files"
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "skill_remote_1",
				"display_title":  r.FormValue("display_title"),
				"latest_version": "v1",
				"source":         "custom",
				"created_at":     now,
				"updated_at":     now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/skills":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":             "pdf",
						"display_title":  "PDF",
						"latest_version": "1",
						"source":         "anthropic",
						"created_at":     now,
						"updated_at":     now,
					},
					{
						"id":             "skill_other_tenant",
						"display_title":  "Secret",
						"latest_version": "1",
						"source":         "custom",
						"created_at":     now,
						"updated_at":     now,
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/skills/skill_remote_1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "skill_remote_1",
				"display_title":  "Release Helper",
				"latest_version": "v2",
				"source":         "custom",
				"created_at":     now,
				"updated_at":     now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/skills/skill_other_tenant":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "skill_other_tenant",
				"display_title":  "Secret",
				"latest_version": "v1",
				"source":         "custom",
				"created_at":     now,
				"updated_at":     now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/skills/skill_remote_1/versions":
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("parse version multipart: %v", err)
			}
			if len(r.MultipartForm.File["files"]) == 0 {
				t.Errorf("expected multipart field named files")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "skver_remote_2",
				"skill_id":    "skill_remote_1",
				"version":     "v2",
				"name":        "release-helper",
				"description": "v2 desc",
				"directory":   "release-helper",
				"created_at":  now.Add(time.Minute),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/skills/skill_remote_1/versions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":          "skver_remote_2",
						"skill_id":    "skill_remote_1",
						"version":     "v2",
						"name":        "release-helper",
						"description": "v2 desc",
						"directory":   "release-helper",
						"created_at":  now.Add(time.Minute),
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/skills/skill_remote_1/versions/v2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "skver_remote_2",
				"skill_id":    "skill_remote_1",
				"version":     "v2",
				"name":        "release-helper",
				"description": "v2 desc",
				"directory":   "release-helper",
				"created_at":  now.Add(time.Minute),
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/skills/skill_remote_1/versions/v2":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/skills/skill_remote_1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/agents":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			rawSkills, _ := json.Marshal(req["skills"])
			_ = json.Unmarshal(rawSkills, &lastAgentSkills)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "agent_remote_sk",
				"version":    1,
				"name":       req["name"],
				"model":      req["model"],
				"skills":     req["skills"],
				"created_at": now,
				"updated_at": now,
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: rt.profile.UUID,
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: rt.profile.UUID,
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	created, err := rt.CreateSkill(ctx, scopeA, agentruntime.CreateSkillInput{
		DisplayTitle: "Release Helper",
		Description:  "helps release",
		Files: []agentruntime.SkillFile{
			{Path: "release-helper/SKILL.md", Data: []byte("---\nname: release-helper\ndescription: helps release\n---\n")},
		},
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if lastUploadField != "files" {
		t.Fatalf("multipart field = %q, want files", lastUploadField)
	}
	if !strings.HasPrefix(created.ID, "skill_") {
		t.Fatalf("created.ID = %q", created.ID)
	}

	if _, err := rt.GetSkill(ctx, scopeB, "skill_other_tenant"); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("other tenant custom skill err = %v, want ErrResourceNotFound", err)
	}
	hitsBeforeDeleteIDOR := upstreamHits.Load()
	if err := rt.DeleteSkill(ctx, scopeB, created.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant DeleteSkill err = %v, want ErrResourceNotFound", err)
	}
	if upstreamHits.Load() != hitsBeforeDeleteIDOR {
		t.Fatalf("cross-tenant DeleteSkill must not hit upstream")
	}

	ver, err := rt.CreateSkillVersion(ctx, scopeA, created.ID, agentruntime.CreateSkillVersionInput{
		Description: "v2 desc",
		Files: []agentruntime.SkillFile{
			{Path: "release-helper/SKILL.md", Data: []byte("---\nname: release-helper\ndescription: v2 desc\n---\n")},
		},
	})
	if err != nil || ver.SkillID != created.ID || ver.Version != "v2" {
		t.Fatalf("CreateSkillVersion = (%+v, %v)", ver, err)
	}

	versions, err := rt.ListSkillVersions(ctx, scopeA, created.ID, agentruntime.ListSkillVersionsQuery{Limit: 10})
	if err != nil || len(versions.Items) != 1 || versions.Items[0].SkillID != created.ID {
		t.Fatalf("ListSkillVersions = (%+v, %v)", versions, err)
	}

	latestVer, err := rt.GetSkillVersion(ctx, scopeA, created.ID, "latest")
	if err != nil || latestVer.Version != "v2" {
		t.Fatalf("GetSkillVersion(latest) = (%+v, %v)", latestVer, err)
	}

	allPage, err := rt.ListSkills(ctx, scopeA, agentruntime.ListSkillsQuery{Limit: 20})
	if err != nil || len(allPage.Items) != 2 {
		t.Fatalf("ListSkills = (%+v, %v), want 1 builtin + 1 owned custom", allPage, err)
	}

	agent, err := rt.CreateAgent(ctx, scopeA, agentruntime.CreateAgentInput{
		Name:   "Skilled Agent",
		Model:  "auto",
		Skills: json.RawMessage(`[{"type":"custom","skill_id":"` + created.ID + `","version":"latest"},{"type":"anthropic","skill_id":"pdf","version":"1"}]`),
	})
	if err != nil {
		t.Fatalf("CreateAgent with skill: %v", err)
	}
	if len(lastAgentSkills) != 2 || lastAgentSkills[0]["skill_id"] != "skill_remote_1" || lastAgentSkills[1]["skill_id"] != "pdf" {
		t.Fatalf("unexpected upstream agent skills: %+v", lastAgentSkills)
	}
	if !strings.Contains(string(agent.Skills), created.ID) {
		t.Fatalf("expected agent skills mapped back to oma id %q: %s", created.ID, string(agent.Skills))
	}

	if err := rt.DeleteSkillVersion(ctx, scopeA, created.ID, "v2"); err != nil {
		t.Fatalf("DeleteSkillVersion: %v", err)
	}
	if err := rt.DeleteSkill(ctx, scopeA, created.ID); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
}
