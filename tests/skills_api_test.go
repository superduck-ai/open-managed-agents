package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type skillAPIResponse struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"created_at"`
	DisplayTitle  string `json:"display_title"`
	LatestVersion string `json:"latest_version"`
	Source        string `json:"source"`
	Type          string `json:"type"`
	UpdatedAt     string `json:"updated_at"`
}

type skillVersionAPIResponse struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"created_at"`
	Description string `json:"description"`
	Directory   string `json:"directory"`
	Name        string `json:"name"`
	SkillID     string `json:"skill_id"`
	Type        string `json:"type"`
	Version     string `json:"version"`
}

type skillPageAPIResponse struct {
	Data     []skillAPIResponse `json:"data"`
	HasMore  bool               `json:"has_more"`
	NextPage *string            `json:"next_page"`
}

type skillVersionPageAPIResponse struct {
	Data     []skillVersionAPIResponse `json:"data"`
	HasMore  bool                      `json:"has_more"`
	NextPage *string                   `json:"next_page"`
}

type skillUploadFile struct {
	FieldName string
	Filename  string
	Content   string
}

func TestSkillsAPI(t *testing.T) {
	store := newFakeStore("fake-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()
	cleanupBuiltinSkillRows(t, app.db)
	defer cleanupBuiltinSkillRows(t, app.db)
	seedBuiltinSkill(t, app, store, "xlsx", "20260203")

	t.Run("failure missing beta header", func(t *testing.T) {
		resp := doSkillRequest(t, app, http.MethodGet, "/v1/skills?beta=true", nil, defaultTestKey, false, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doSkillRequest(t, app, http.MethodGet, "/v1/skills", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure non multipart upload", func(t *testing.T) {
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", strings.NewReader(`{"files":[]}`), defaultTestKey, true, "application/json")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing files field", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "", nil)
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure empty archive upload", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "empty.zip", Content: ""},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("empty archive status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var payload errorResponse
		decodeJSON(t, resp.Body, &payload)
		if payload.Error.Type != "invalid_request_error" || payload.Error.Message != "Skill package must contain files" {
			t.Fatalf("unexpected empty archive error: %+v", payload)
		}
	})

	t.Run("failure missing top-level skill md", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "my-skill/README.md", Content: "# nope"},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure multiple top-level directories", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "one/SKILL.md", Content: "# One"},
			{FieldName: "files[]", Filename: "two/file.txt", Content: "two"},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure path traversal", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "../bad/SKILL.md", Content: "# Bad"},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure duplicate display title uses update flow", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "Duplicate Skill", []skillUploadFile{
			{FieldName: "files[]", Filename: "duplicate-skill/SKILL.md", Content: "---\nname: Duplicate Skill\ndescription: first\n---\n\n# Duplicate Skill\n"},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create duplicate fixture status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var created skillAPIResponse
		decodeJSON(t, resp.Body, &created)
		defer deleteSkill(t, app, created.ID)

		body, contentType = skillMultipartBody(t, "Duplicate Skill", []skillUploadFile{
			{FieldName: "files[]", Filename: "duplicate-skill-copy/SKILL.md", Content: "---\nname: Duplicate Skill\ndescription: second\n---\n\n# Duplicate Skill\n"},
		})
		resp = doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("duplicate create status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var conflict errorResponse
		decodeJSON(t, resp.Body, &conflict)
		if conflict.Error.Type != "invalid_request_error" || !strings.Contains(conflict.Error.Message, "Skill cannot reuse an existing display_title: Duplicate Skill") {
			t.Fatalf("unexpected duplicate title error: %+v", conflict)
		}
	})

	t.Run("success custom skill archive upload", func(t *testing.T) {
		archive := skillArchiveBytes(t, "archive-skill", "---\nname: Archive Skill\ndescription: archive description\n---\n\n# Archive Skill\n")
		body, contentType := skillMultipartBody(t, "Archive Skill", []skillUploadFile{
			{FieldName: "files[]", Filename: "archive-skill.skill", Content: string(archive)},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create archive skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var created skillAPIResponse
		decodeJSON(t, resp.Body, &created)
		defer deleteSkill(t, app, created.ID)
		versions := listSkillVersions(t, app, created.ID, "")
		if len(versions.Data) != 1 || versions.Data[0].Directory != "archive-skill" || versions.Data[0].Name != "Archive Skill" {
			t.Fatalf("unexpected archive upload versions: %+v", versions)
		}
	})

	t.Run("success custom skill upload with platform session", func(t *testing.T) {
		sessionKey := "session-skills-platform"
		app.seedPlatformSession(t, sessionKey)
		body, contentType := skillMultipartBody(t, "Session Skill", []skillUploadFile{
			{FieldName: "files[]", Filename: "session-skill/SKILL.md", Content: "---\nname: Session Skill\ndescription: session description\n---\n\n# Session Skill\n"},
		})
		resp := doSkillSessionRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, sessionKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create platform session skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var created skillAPIResponse
		decodeJSON(t, resp.Body, &created)
		defer deleteSkill(t, app, created.ID)
		if created.DisplayTitle != "Session Skill" || created.Source != "custom" {
			t.Fatalf("unexpected platform session skill: %+v", created)
		}
	})

	t.Run("success custom skill version upload with platform session", func(t *testing.T) {
		created := createSkill(t, app, "session-update-skill")
		defer deleteSkill(t, app, created.ID)

		sessionKey := "session-skills-platform-update"
		app.seedPlatformSession(t, sessionKey)
		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "session-update-skill/SKILL.md", Content: "---\nname: Session Update Skill\ndescription: session update description\n---\n\n# Session Update Skill\n"},
		})
		resp := doSkillSessionRequest(t, app, http.MethodPost, "/v1/skills/"+created.ID+"/versions?beta=true", body, sessionKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create platform session skill version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var version skillVersionAPIResponse
		decodeJSON(t, resp.Body, &version)
		if version.SkillID != created.ID || version.Name != "Session Update Skill" || version.Version == created.LatestVersion {
			t.Fatalf("unexpected platform session skill version: %+v", version)
		}
	})

	t.Run("failure cross workspace isolation", func(t *testing.T) {
		otherKey := "sk-ant-local-skills-other"
		seedWorkspaceKey(t, app.db, "org_skills_other_test", "workspace_skills_other_test", "api_key_skills_other_test", otherKey)

		created := createSkill(t, app, "cross-workspace-skill")
		defer deleteSkill(t, app, created.ID)

		resp := doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+created.ID+"?beta=true", nil, otherKey, true, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("success built-in public skills", func(t *testing.T) {
		page := listSkills(t, app, "source=anthropic&limit=100")
		if !containsSkill(page.Data, "xlsx") {
			t.Fatalf("built-in skills did not include xlsx: %+v", page.Data)
		}
		for _, skill := range page.Data {
			if skill.Source != "anthropic" || skill.Type != "skill" || skill.LatestVersion != "20260203" {
				t.Fatalf("unexpected built-in skill: %+v", skill)
			}
		}

		unknown := listSkills(t, app, "source=source&page=page&limit=0")
		if len(unknown.Data) != 0 || unknown.HasMore {
			t.Fatalf("unknown source page = %+v, want empty", unknown)
		}

		resp := doSkillRequest(t, app, http.MethodGet, "/v1/skills/xlsx?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve xlsx status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var xlsx skillAPIResponse
		decodeJSON(t, resp.Body, &xlsx)
		if xlsx.ID != "xlsx" || xlsx.Source != "anthropic" || xlsx.LatestVersion != "20260203" {
			t.Fatalf("unexpected xlsx response: %+v", xlsx)
		}

		versions := listSkillVersions(t, app, "xlsx", "")
		if len(versions.Data) != 1 || versions.Data[0].Version != "20260203" || versions.Data[0].Directory == "" {
			t.Fatalf("unexpected xlsx versions: %+v", versions)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/xlsx/versions/20260203/content?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download xlsx status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		data := readAll(t, resp.Body)
		assertZipContains(t, data, "xlsx/SKILL.md")

		resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/xlsx?beta=true", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/xlsx/versions/20260203?beta=true", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure list rejects invalid pagination params", func(t *testing.T) {
		for _, path := range []string{
			"/v1/skills?beta=true&page=bad",
			"/v1/skills?beta=true&source=anthropic&page=bad",
			"/v1/skills?beta=true&source=custom&page=bad",
			"/v1/skills?beta=true&limit=-1",
			"/v1/skills?beta=true&limit=101",
			"/v1/skills?beta=true&limit=abc",
		} {
			resp := doSkillRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true, "")
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
	})

	t.Run("success custom skill lifecycle", func(t *testing.T) {
		created := createSkill(t, app, "custom-skill")
		if created.Type != "skill" || created.Source != "custom" || created.DisplayTitle != "Custom Skill" || created.LatestVersion == "" {
			t.Fatalf("unexpected create response: %+v", created)
		}

		customPage := listSkills(t, app, "source=custom&limit=20")
		if !containsSkill(customPage.Data, created.ID) {
			t.Fatalf("custom list did not include %s: %+v", created.ID, customPage.Data)
		}

		resp := doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+created.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve custom status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var retrieved skillAPIResponse
		decodeJSON(t, resp.Body, &retrieved)
		if retrieved.ID != created.ID || retrieved.LatestVersion != created.LatestVersion {
			t.Fatalf("unexpected retrieve response: %+v", retrieved)
		}

		versions := listSkillVersions(t, app, created.ID, "")
		if len(versions.Data) != 1 || versions.Data[0].Version != created.LatestVersion || versions.Data[0].Directory != "custom-skill" {
			t.Fatalf("unexpected initial versions: %+v", versions)
		}
		firstVersion := versions.Data[0]

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+created.ID+"/versions/latest?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve latest version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var latest skillVersionAPIResponse
		decodeJSON(t, resp.Body, &latest)
		if latest.Version != firstVersion.Version {
			t.Fatalf("latest version = %s, want %s", latest.Version, firstVersion.Version)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+created.ID+"/versions/"+firstVersion.Version+"/content?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download custom status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		assertZipContains(t, readAll(t, resp.Body), "custom-skill/SKILL.md")

		time.Sleep(time.Millisecond)
		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files", Filename: "custom-skill/SKILL.md", Content: "---\nname: Custom Skill v2\ndescription: v2 description\n---\n\n# Custom Skill v2"},
		})
		resp = doSkillRequest(t, app, http.MethodPost, "/v1/skills/"+created.ID+"/versions?beta=true", body, defaultTestKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var secondVersion skillVersionAPIResponse
		decodeJSON(t, resp.Body, &secondVersion)
		if secondVersion.SkillID != created.ID || secondVersion.Version == firstVersion.Version || secondVersion.Name != "Custom Skill v2" {
			t.Fatalf("unexpected second version: %+v", secondVersion)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+created.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve after version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var afterVersion skillAPIResponse
		decodeJSON(t, resp.Body, &afterVersion)
		if afterVersion.LatestVersion != secondVersion.Version {
			t.Fatalf("latest_version = %s, want %s", afterVersion.LatestVersion, secondVersion.Version)
		}

		resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+created.ID+"/versions/"+secondVersion.Version+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var deletedVersion map[string]string
		decodeJSON(t, resp.Body, &deletedVersion)
		if deletedVersion["id"] != secondVersion.Version || deletedVersion["type"] != "skill_version_deleted" {
			t.Fatalf("unexpected delete version response: %+v", deletedVersion)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+created.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve after delete version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var afterDeleteVersion skillAPIResponse
		decodeJSON(t, resp.Body, &afterDeleteVersion)
		if afterDeleteVersion.LatestVersion != firstVersion.Version {
			t.Fatalf("latest_version after delete = %s, want %s", afterDeleteVersion.LatestVersion, firstVersion.Version)
		}

		resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+created.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var deletedSkill map[string]string
		decodeJSON(t, resp.Body, &deletedSkill)
		if deletedSkill["id"] != created.ID || deletedSkill["type"] != "skill_deleted" {
			t.Fatalf("unexpected delete skill response: %+v", deletedSkill)
		}
	})

	t.Run("success delete object queues cleanup job", func(t *testing.T) {
		cleanupStore := newFakeStore("fake-bucket")
		cleanupStore.deleteErr = errors.New("minio unavailable")
		cleanupApp := newTestAppWithStore(t, nil, cleanupStore)
		defer cleanupApp.close()

		created := createSkill(t, cleanupApp, "cleanup-skill")
		var objectKey string
		for key := range cleanupStore.objects {
			objectKey = key
			break
		}
		if objectKey == "" {
			t.Fatalf("expected skill object to be stored")
		}
		defer cleanupApp.db.Pool.Exec(context.Background(), `delete from jobs where payload->>'key' = $1`, objectKey)

		resp := doSkillRequest(t, cleanupApp, http.MethodDelete, "/v1/skills/"+created.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete cleanup skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		var jobCount int
		if err := cleanupApp.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from jobs
			where type = 'object_cleanup'
				and status = 'pending'
				and payload->>'key' = $1
				and payload->>'file_id' like 'skillver_%'
		`, objectKey).Scan(&jobCount); err != nil {
			t.Fatalf("count cleanup jobs: %v", err)
		}
		if jobCount != 1 {
			t.Fatalf("cleanup job count = %d, want 1", jobCount)
		}
	})

	t.Run("success environment credential can read and download", func(t *testing.T) {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		env := createEnvironment(t, app, `{"name":"skills-env-key-env-`+suffix+`"}`)
		envKey := createEnvironmentKeyForTest(t, app, env.ID, "sk-ant-env-skills-"+suffix)

		resp := doSkillRequest(t, app, http.MethodGet, "/v1/skills?beta=true&source=anthropic&limit=1", nil, envKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("env key list skills status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var page skillPageAPIResponse
		decodeJSON(t, resp.Body, &page)
		if len(page.Data) != 1 || page.Data[0].Source != "anthropic" {
			t.Fatalf("unexpected env key list page: %+v", page)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/xlsx/versions/20260203/content?beta=true", nil, envKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("env key download skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		assertZipContains(t, readAll(t, resp.Body), "xlsx/SKILL.md")

		body, contentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "env-write/SKILL.md", Content: "# Env Write"},
		})
		resp = doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, envKey, true, contentType)
		assertError(t, resp, http.StatusForbidden, "permission_error")
	})

	t.Run("success official sdk fixture compatibility", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "display_title", []skillUploadFile{
			{FieldName: "files[]", Filename: "anonymous_file", Content: "Example data"},
		})
		resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, config.OfficialSDKResourceAPIKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official create status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var created skillAPIResponse
		decodeJSON(t, resp.Body, &created)
		if created.ID != app.cfg.OfficialSDKFixtureSkillID {
			t.Fatalf("official create id = %s, want %s", created.ID, app.cfg.OfficialSDKFixtureSkillID)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+app.cfg.OfficialSDKFixtureSkillID+"?beta=true", nil, config.OfficialSDKResourceAPIKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official retrieve status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		officialList := listSkillsWithKey(t, app, "source=source&page=page&limit=0", config.OfficialSDKResourceAPIKey)
		if len(officialList.Data) != 0 {
			t.Fatalf("official unknown source list = %+v, want empty", officialList)
		}

		versionBody, versionContentType := skillMultipartBody(t, "", []skillUploadFile{
			{FieldName: "files[]", Filename: "anonymous_file", Content: "Example data"},
		})
		resp = doSkillRequest(t, app, http.MethodPost, "/v1/skills/"+app.cfg.OfficialSDKFixtureSkillID+"/versions?beta=true", versionBody, config.OfficialSDKResourceAPIKey, true, versionContentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official create version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		officialVersions := listSkillVersionsWithKey(t, app, app.cfg.OfficialSDKFixtureSkillID, "page=page&limit=0", config.OfficialSDKResourceAPIKey)
		if len(officialVersions.Data) != 1 || officialVersions.Data[0].Version != app.cfg.OfficialSDKFixtureSkillVersion {
			t.Fatalf("official versions = %+v", officialVersions)
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+app.cfg.OfficialSDKFixtureSkillID+"/versions/"+app.cfg.OfficialSDKFixtureSkillVersion+"?beta=true", nil, config.OfficialSDKResourceAPIKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official retrieve version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		resp = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+app.cfg.OfficialSDKFixtureSkillID+"/versions/"+app.cfg.OfficialSDKFixtureSkillVersion+"/content?beta=true", nil, config.OfficialSDKResourceAPIKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official download version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		assertZipContains(t, readAll(t, resp.Body), "fixture-skill/SKILL.md")

		resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+app.cfg.OfficialSDKFixtureSkillID+"/versions/"+app.cfg.OfficialSDKFixtureSkillVersion+"?beta=true", nil, config.OfficialSDKResourceAPIKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official delete version status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		resp = doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+app.cfg.OfficialSDKFixtureSkillID+"?beta=true", nil, config.OfficialSDKResourceAPIKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("official delete skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})
}

func TestSkillsDisplayTitleSchema(t *testing.T) {
	store := newFakeStore("skills-schema-bucket")
	app := newTestAppWithStore(t, nil, store)
	defer app.close()

	var isNullable string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select is_nullable
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'skills'
			and column_name = 'display_title'
	`).Scan(&isNullable); err != nil {
		t.Fatalf("query skills.display_title nullability: %v", err)
	}
	if isNullable != "NO" {
		t.Fatalf("skills.display_title is_nullable = %s, want NO", isNullable)
	}

	var indexDef string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select indexdef
		from pg_indexes
		where schemaname = current_schema()
			and indexname = 'skills_workspace_display_title_active_key'
	`).Scan(&indexDef); err != nil {
		t.Fatalf("query skills display_title unique index: %v", err)
	}
	normalizedIndexDef := strings.ToLower(indexDef)
	if !strings.Contains(normalizedIndexDef, "display_title") || !strings.Contains(normalizedIndexDef, "where (deleted_at is null)") {
		t.Fatalf("skills display_title index = %s, want active display_title unique index", indexDef)
	}
	if strings.Contains(normalizedIndexDef, "display_title is not null") {
		t.Fatalf("skills display_title index = %s, should not depend on nullable display_title predicate", indexDef)
	}
}

func TestSkillsListPagination(t *testing.T) {
	t.Run("success mixed list continues after builtin boundary", func(t *testing.T) {
		pageApp, pageStore := newSkillsPaginationTestApp(t)
		suffix := uniqueSkillSuffix()
		seedBuiltinSkill(t, pageApp, pageStore, "mixed-boundary-a", "20260203")
		seedBuiltinSkill(t, pageApp, pageStore, "mixed-boundary-b", "20260203")
		created := createNamedSkill(t, pageApp, "Mixed Pagination Skill "+suffix, "mixed-pagination-skill-"+suffix)
		defer deleteSkill(t, pageApp, created.ID)

		first := listSkills(t, pageApp, "limit=1")
		if len(first.Data) != 1 || first.Data[0].Source != "anthropic" || !first.HasMore || first.NextPage == nil {
			t.Fatalf("first mixed page = %+v, want one builtin with next_page", first)
		}
		second := listSkills(t, pageApp, "limit=1&page="+url.QueryEscape(*first.NextPage))
		if len(second.Data) != 1 || second.Data[0].Source != "anthropic" || !second.HasMore || second.NextPage == nil {
			t.Fatalf("second mixed page = %+v, want final builtin with next_page to custom", second)
		}
		third := listSkills(t, pageApp, "limit=1&page="+url.QueryEscape(*second.NextPage))
		if len(third.Data) != 1 || third.Data[0].ID != created.ID || third.Data[0].Source != "custom" || third.HasMore || third.NextPage != nil {
			t.Fatalf("third mixed page = %+v, want final custom skill %s", third, created.ID)
		}
	})

	t.Run("success mixed list fills remaining page with custom skills", func(t *testing.T) {
		pageApp, pageStore := newSkillsPaginationTestApp(t)
		suffix := uniqueSkillSuffix()
		seedBuiltinSkill(t, pageApp, pageStore, "mixed-fill-builtin", "20260203")
		firstCustom := createNamedSkill(t, pageApp, "Mixed Fill One "+suffix, "mixed-fill-one-"+suffix)
		defer deleteSkill(t, pageApp, firstCustom.ID)
		secondCustom := createNamedSkill(t, pageApp, "Mixed Fill Two "+suffix, "mixed-fill-two-"+suffix)
		defer deleteSkill(t, pageApp, secondCustom.ID)
		thirdCustom := createNamedSkill(t, pageApp, "Mixed Fill Three "+suffix, "mixed-fill-three-"+suffix)
		defer deleteSkill(t, pageApp, thirdCustom.ID)

		first := listSkills(t, pageApp, "limit=2")
		if len(first.Data) != 2 || first.Data[0].Source != "anthropic" || first.Data[1].Source != "custom" || !first.HasMore || first.NextPage == nil {
			t.Fatalf("first mixed fill page = %+v, want builtin plus custom with next_page", first)
		}
		if first.Data[1].ID != thirdCustom.ID {
			t.Fatalf("first mixed fill custom = %s, want newest custom %s", first.Data[1].ID, thirdCustom.ID)
		}

		second := listSkills(t, pageApp, "limit=2&page="+url.QueryEscape(*first.NextPage))
		if len(second.Data) != 2 || second.Data[0].ID != secondCustom.ID || second.Data[1].ID != firstCustom.ID || second.HasMore || second.NextPage != nil {
			t.Fatalf("second mixed fill page = %+v, want remaining custom skills and no next_page", second)
		}
	})

	t.Run("success mixed list stops at builtin boundary without custom skills", func(t *testing.T) {
		pageApp, pageStore := newSkillsPaginationTestApp(t)
		seedBuiltinSkill(t, pageApp, pageStore, "builtin-only-a", "20260203")
		seedBuiltinSkill(t, pageApp, pageStore, "builtin-only-b", "20260203")

		first := listSkills(t, pageApp, "limit=1")
		if len(first.Data) != 1 || first.Data[0].Source != "anthropic" || !first.HasMore || first.NextPage == nil {
			t.Fatalf("first builtin-only page = %+v, want first builtin with next_page", first)
		}
		second := listSkills(t, pageApp, "limit=1&page="+url.QueryEscape(*first.NextPage))
		if len(second.Data) != 1 || second.Data[0].Source != "anthropic" || second.HasMore || second.NextPage != nil {
			t.Fatalf("second builtin-only page = %+v, want final builtin with no next_page", second)
		}
	})

	t.Run("success source filtered skill lists paginate independently", func(t *testing.T) {
		pageApp, pageStore := newSkillsPaginationTestApp(t)
		suffix := uniqueSkillSuffix()
		seedBuiltinSkill(t, pageApp, pageStore, "source-page-a", "20260203")
		seedBuiltinSkill(t, pageApp, pageStore, "source-page-b", "20260203")
		firstCustom := createNamedSkill(t, pageApp, "Source Page One "+suffix, "source-page-one-"+suffix)
		defer deleteSkill(t, pageApp, firstCustom.ID)
		secondCustom := createNamedSkill(t, pageApp, "Source Page Two "+suffix, "source-page-two-"+suffix)
		defer deleteSkill(t, pageApp, secondCustom.ID)

		anthropicFirst := listSkills(t, pageApp, "source=anthropic&limit=1")
		if len(anthropicFirst.Data) != 1 || anthropicFirst.Data[0].Source != "anthropic" || !anthropicFirst.HasMore || anthropicFirst.NextPage == nil {
			t.Fatalf("first anthropic source page = %+v, want builtin with next_page", anthropicFirst)
		}
		anthropicSecond := listSkills(t, pageApp, "source=anthropic&limit=1&page="+url.QueryEscape(*anthropicFirst.NextPage))
		if len(anthropicSecond.Data) != 1 || anthropicSecond.Data[0].Source != "anthropic" || anthropicSecond.HasMore || anthropicSecond.NextPage != nil {
			t.Fatalf("second anthropic source page = %+v, want final builtin", anthropicSecond)
		}

		customFirst := listSkills(t, pageApp, "source=custom&limit=1")
		if len(customFirst.Data) != 1 || customFirst.Data[0].ID != secondCustom.ID || customFirst.Data[0].Source != "custom" || !customFirst.HasMore || customFirst.NextPage == nil {
			t.Fatalf("first custom source page = %+v, want newest custom with next_page", customFirst)
		}
		customSecond := listSkills(t, pageApp, "source=custom&limit=1&page="+url.QueryEscape(*customFirst.NextPage))
		if len(customSecond.Data) != 1 || customSecond.Data[0].ID != firstCustom.ID || customSecond.Data[0].Source != "custom" || customSecond.HasMore || customSecond.NextPage != nil {
			t.Fatalf("second custom source page = %+v, want oldest custom", customSecond)
		}
	})
}

func doSkillRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new skills request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if betaHeader {
		req.Header.Set("anthropic-beta", "skills-2025-10-02")
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do skills request: %v", err)
	}
	return resp
}

func doSkillSessionRequest(t *testing.T, app *testApp, method, path string, body io.Reader, sessionKey string, betaHeader bool, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new skills session request: %v", err)
	}
	if sessionKey != "" {
		req.AddCookie(&http.Cookie{Name: "sessionKey", Value: sessionKey})
	}
	if betaHeader {
		req.Header.Set("anthropic-beta", "skills-2025-10-02")
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do skills session request: %v", err)
	}
	return resp
}

func skillMultipartBody(t *testing.T, displayTitle string, files []skillUploadFile) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if displayTitle != "" {
		if err := writer.WriteField("display_title", displayTitle); err != nil {
			t.Fatalf("write display_title: %v", err)
		}
	}
	for _, file := range files {
		fieldName := file.FieldName
		if fieldName == "" {
			fieldName = "files[]"
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, file.Filename))
		header.Set("Content-Type", "application/octet-stream")
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create multipart part: %v", err)
		}
		if _, err := part.Write([]byte(file.Content)); err != nil {
			t.Fatalf("write multipart part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func createSkill(t *testing.T, app *testApp, directory string) skillAPIResponse {
	t.Helper()
	body, contentType := skillMultipartBody(t, "Custom Skill", []skillUploadFile{
		{FieldName: "files[]", Filename: directory + "/SKILL.md", Content: "---\nname: Custom Skill\ndescription: custom description\n---\n\n# Custom Skill\n"},
		{FieldName: "files", Filename: directory + "/README.md", Content: "readme"},
	})
	resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var created skillAPIResponse
	decodeJSON(t, resp.Body, &created)
	return created
}

func createNamedSkill(t *testing.T, app *testApp, displayTitle, directory string) skillAPIResponse {
	t.Helper()
	body, contentType := skillMultipartBody(t, displayTitle, []skillUploadFile{
		{FieldName: "files[]", Filename: directory + "/SKILL.md", Content: "---\nname: " + displayTitle + "\ndescription: custom description\n---\n\n# " + displayTitle + "\n"},
		{FieldName: "files", Filename: directory + "/README.md", Content: "readme"},
	})
	resp := doSkillRequest(t, app, http.MethodPost, "/v1/skills?beta=true", body, defaultTestKey, true, contentType)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create named skill status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var created skillAPIResponse
	decodeJSON(t, resp.Body, &created)
	return created
}

func newSkillsPaginationTestApp(t *testing.T) (*testApp, *fakeStore) {
	t.Helper()
	store := newFakeStore("skills-pagination-bucket-" + uniqueSkillSuffix())
	app := newTestAppWithStore(t, nil, store)
	cleanupBuiltinSkillRows(t, app.db)
	t.Cleanup(func() {
		cleanupBuiltinSkillRows(t, app.db)
		app.close()
	})
	return app, store
}

func uniqueSkillSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func deleteSkill(t *testing.T, app *testApp, skillID string) {
	t.Helper()
	resp := doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+skillID+"?beta=true", nil, defaultTestKey, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete skill cleanup status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listSkills(t *testing.T, app *testApp, query string) skillPageAPIResponse {
	t.Helper()
	return listSkillsWithKey(t, app, query, defaultTestKey)
}

func listSkillsWithKey(t *testing.T, app *testApp, query, key string) skillPageAPIResponse {
	t.Helper()
	path := "/v1/skills?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doSkillRequest(t, app, http.MethodGet, path, nil, key, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list skills status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page skillPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func listSkillVersions(t *testing.T, app *testApp, skillID, query string) skillVersionPageAPIResponse {
	t.Helper()
	return listSkillVersionsWithKey(t, app, skillID, query, defaultTestKey)
}

func listSkillVersionsWithKey(t *testing.T, app *testApp, skillID, query, key string) skillVersionPageAPIResponse {
	t.Helper()
	path := "/v1/skills/" + skillID + "/versions?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doSkillRequest(t, app, http.MethodGet, path, nil, key, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list skill versions status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page skillVersionPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func containsSkill(skills []skillAPIResponse, id string) bool {
	for _, skill := range skills {
		if skill.ID == id {
			return true
		}
	}
	return false
}

func seedBuiltinSkill(t *testing.T, app *testApp, store *fakeStore, skillID, version string) {
	t.Helper()
	archive := skillArchiveBytes(t, skillID, "---\nname: "+skillID+"\ndescription: builtin "+skillID+"\n---\n\n# "+skillID+"\n")
	sum := sha256.Sum256(archive)
	shaHex := hex.EncodeToString(sum[:])
	key := "builtin-skills/" + skillID + "/versions/" + version + "/" + shaHex + ".skill"
	if err := store.Put(context.Background(), key, bytes.NewReader(archive), int64(len(archive)), "application/zip"); err != nil {
		t.Fatalf("seed builtin object: %v", err)
	}
	now := time.Now().UTC()
	if _, _, err := app.db.UpsertBuiltinSkillWithVersion(context.Background(), db.BuiltinSkill{
		ExternalID:   skillID,
		DisplayTitle: skillID,
		CreatedAt:    now,
	}, db.BuiltinSkillVersion{
		ExternalID:  "skillver_" + skillID + "_" + version,
		Version:     version,
		Name:        skillID,
		Description: "builtin " + skillID,
		Directory:   skillID,
		S3Bucket:    store.Bucket(),
		S3Key:       key,
		SizeBytes:   int64(len(archive)),
		SHA256:      shaHex,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("seed builtin db: %v", err)
	}
}

func skillArchiveBytes(t *testing.T, directory, skillMD string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create(directory + "/SKILL.md")
	if err != nil {
		t.Fatalf("create skill md: %v", err)
	}
	if _, err := entry.Write([]byte(skillMD)); err != nil {
		t.Fatalf("write skill md: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close skill archive: %v", err)
	}
	return buf.Bytes()
}

func assertZipContains(t *testing.T, data []byte, name string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, file := range reader.File {
		if file.Name == name {
			return
		}
	}
	t.Fatalf("zip did not contain %s", name)
}
