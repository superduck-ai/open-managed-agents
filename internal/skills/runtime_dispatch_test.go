package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeSkillProfileStore struct {
	profile db.RuntimeProfile
}

func (f *fakeSkillProfileStore) GetRuntimeProfileByUUID(_ context.Context, _ string) (db.RuntimeProfile, error) {
	return f.profile, nil
}

func (f *fakeSkillProfileStore) GetWorkspaceRuntimeProfile(_ context.Context, _, _ string) (db.RuntimeProfile, error) {
	return f.profile, nil
}

func (f *fakeSkillProfileStore) UpdateRuntimeProfileHealth(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}

type fakeExternalSkillRuntime struct {
	lastCreate  agentruntime.CreateSkillInput
	lastVersion agentruntime.CreateSkillVersionInput
	skill       agentruntime.Skill
	version     agentruntime.SkillVersion
	deleted     bool
	verDeleted  bool
}

func (f *fakeExternalSkillRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (f *fakeExternalSkillRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (f *fakeExternalSkillRuntime) Close() error { return nil }

func (f *fakeExternalSkillRuntime) CreateSkill(_ context.Context, _ agentruntime.Scope, input agentruntime.CreateSkillInput) (agentruntime.Skill, error) {
	f.lastCreate = input
	return f.skill, nil
}

func (f *fakeExternalSkillRuntime) GetSkill(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Skill, error) {
	return f.skill, nil
}

func (f *fakeExternalSkillRuntime) ListSkills(_ context.Context, _ agentruntime.Scope, _ agentruntime.ListSkillsQuery) (agentruntime.Page[agentruntime.Skill], error) {
	return agentruntime.Page[agentruntime.Skill]{Items: []agentruntime.Skill{f.skill}}, nil
}

func (f *fakeExternalSkillRuntime) DeleteSkill(_ context.Context, _ agentruntime.Scope, _ string) error {
	f.deleted = true
	return nil
}

func (f *fakeExternalSkillRuntime) CreateSkillVersion(_ context.Context, _ agentruntime.Scope, _ string, input agentruntime.CreateSkillVersionInput) (agentruntime.SkillVersion, error) {
	f.lastVersion = input
	return f.version, nil
}

func (f *fakeExternalSkillRuntime) ListSkillVersions(_ context.Context, _ agentruntime.Scope, _ string, _ agentruntime.ListSkillVersionsQuery) (agentruntime.Page[agentruntime.SkillVersion], error) {
	return agentruntime.Page[agentruntime.SkillVersion]{Items: []agentruntime.SkillVersion{f.version}}, nil
}

func (f *fakeExternalSkillRuntime) GetSkillVersion(_ context.Context, _ agentruntime.Scope, _, _ string) (agentruntime.SkillVersion, error) {
	return f.version, nil
}

func (f *fakeExternalSkillRuntime) DeleteSkillVersion(_ context.Context, _ agentruntime.Scope, _, _ string) error {
	f.verDeleted = true
	return nil
}

func buildTestSkillMultipart(t *testing.T, displayTitle string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if displayTitle != "" {
		_ = writer.WriteField("display_title", displayTitle)
	}
	part, err := writer.CreateFormFile("files[]", "my-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("---\nname: my-skill\ndescription: Skill desc\n---\n# My Skill\n"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func TestExternalSkillsAndConsoleDispatch(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	fakeRT := &fakeExternalSkillRuntime{
		skill: agentruntime.Skill{
			ID:            "skill_ext_1",
			DisplayTitle:  "My Skill",
			Description:   "Skill desc",
			LatestVersion: "v1",
			Source:        "custom",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		version: agentruntime.SkillVersion{
			ID:          "skver_ext_1",
			SkillID:     "skill_ext_1",
			Version:     "v1",
			Name:        "my-skill",
			Description: "Skill desc",
			Directory:   "my-skill",
			CreatedAt:   now,
		},
	}

	reg := agentruntime.NewRegistry()
	if err := reg.Register(agentruntime.ProviderQoderCloud, func(_ context.Context, _ agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return fakeRT, nil
	}); err != nil {
		t.Fatal(err)
	}
	resolver := agentruntime.NewResolver(&fakeSkillProfileStore{
		profile: db.RuntimeProfile{
			UUID:         "00000000-0000-4000-8000-000000000066",
			ProviderKind: agentruntime.ProviderQoderCloud.String(),
			Status:       "active",
		},
	}, reg, nil, nil)

	handler := NewHandler(nil, nil, nil).WithRuntimeResolver(resolver)
	principal := auth.Principal{
		OrganizationUUID: "org-1",
		WorkspaceUUID:    "ws-1",
		CredentialType:   auth.CredentialTypePlatformSession,
		RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
	}

	body, ct := buildTestSkillMultipart(t, "My Skill")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create skill status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(fakeRT.lastCreate.Files) != 1 || fakeRT.lastCreate.Files[0].Path != "my-skill/SKILL.md" {
		t.Fatalf("unexpected extracted files: %+v", fakeRT.lastCreate.Files)
	}

	req = httptest.NewRequest(http.MethodGet, "/?source=custom", nil)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list skills status = %d body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/skill_ext_1", nil)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get skill status = %d body = %s", rec.Code, rec.Body.String())
	}

	verBody, verCT := buildTestSkillMultipart(t, "")
	req = httptest.NewRequest(http.MethodPost, "/skill_ext_1/versions", verBody)
	req.Header.Set("Content-Type", verCT)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(fakeRT.lastVersion.Files) != 1 {
		t.Fatalf("create version status = %d body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/skill_ext_1/versions", nil)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status = %d body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/skill_ext_1/versions/v1", nil)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version status = %d body = %s", rec.Code, rec.Body.String())
	}

	consoleHandler := handler.ConsoleRoutes()
	req = httptest.NewRequest(http.MethodGet, "/?query=My&source=custom", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	consoleHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("console catalog status = %d body = %s", rec.Code, rec.Body.String())
	}
	var catalog struct {
		TotalCount int `json:"total_count"`
		Counts     struct {
			Custom int `json:"custom"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil || catalog.TotalCount != 1 || catalog.Counts.Custom != 1 {
		t.Fatalf("unexpected console catalog: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/skill_ext_1/versions/v1", nil)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !fakeRT.verDeleted {
		t.Fatalf("delete version status = %d deleted=%v", rec.Code, fakeRT.verDeleted)
	}

	req = httptest.NewRequest(http.MethodDelete, "/skill_ext_1", nil)
	req.Header.Set("anthropic-beta", skillsBeta)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !fakeRT.deleted {
		t.Fatalf("delete skill status = %d deleted=%v", rec.Code, fakeRT.deleted)
	}
}
