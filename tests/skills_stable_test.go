package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type stableSkillAPIResponse struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	LatestVersionID string `json:"latest_version_id"`
	Source          struct {
		Type string `json:"type"`
	} `json:"source"`
}

func stableSkillBody(t *testing.T, label, name string) (*bytes.Buffer, string) {
	t.Helper()
	body, contentType := skillMultipartBody(t, label, []skillUploadFile{
		{FieldName: "files[]", Filename: "SKILL.md", Content: "---\nname: " + name + "\ndescription: stable contract\n---\nInstructions"},
		{FieldName: "files[]", Filename: "references/help.txt", Content: "reference"},
	})
	return bytes.NewBuffer(bytes.ReplaceAll(body.Bytes(), []byte(`name="display_title"`), []byte(`name="display_name"`))), contentType
}

func TestSkillsStableContract(t *testing.T) {
	store := newFakeStore("skills-stable")
	app := newPayloadIntegrationApp(t, store)
	create := func(label, name string) stableSkillAPIResponse {
		t.Helper()
		body, contentType := stableSkillBody(t, label, name)
		response := doSkillRequest(t, app, http.MethodPost, "/v1/skills", body, defaultTestKey, false, contentType)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("create: %d %s", response.StatusCode, readAll(t, response.Body))
		}
		var skill stableSkillAPIResponse
		decodeJSON(t, response.Body, &skill)
		t.Cleanup(func() { deleteSkill(t, app, skill.ID) })
		return skill
	}
	t.Run("reject invalid formal pagination and unsafe upload", func(t *testing.T) {
		for _, query := range []string{"limit=0", "limit=1001", "page=invalid"} {
			response := doSkillRequest(t, app, http.MethodGet, "/v1/skills?"+query, nil, defaultTestKey, false, "")
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		}
		body, contentType := stableSkillBody(t, "bad", "../../escape")
		response := doSkillRequest(t, app, http.MethodPost, "/v1/skills", body, defaultTestKey, false, contentType)
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
	})
	t.Run("display name defaults and validation", func(t *testing.T) {
		skill := create("", "default-label")
		if skill.DisplayName != "default-label" {
			t.Fatalf("derived label: %+v", skill)
		}
		longLabel := strings.Repeat("名", 255)
		skill = create(longLabel, "long-label")
		if skill.DisplayName != longLabel {
			t.Fatal("long Unicode label was changed")
		}
		for _, label := range []string{strings.Repeat("x", 256), "first\nsecond"} {
			body, contentType := stableSkillBody(t, label, "invalid-label")
			response := doSkillRequest(t, app, http.MethodPost, "/v1/skills", body, defaultTestKey, false, contentType)
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		}
	})
	t.Run("builtin source and combined headers", func(t *testing.T) {
		seedBuiltinSkill(t, app, store, "stable-example", "20260101")
		for _, legacy := range []bool{false, true} {
			request, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/skills/stable-example", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Api-Key", defaultTestKey)
			request.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
			if legacy {
				request.Header.Add("anthropic-beta", "other-beta, skills-2025-10-02")
			}
			response, err := app.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 {
				t.Fatalf("combined headers: %d %s", response.StatusCode, readAll(t, response.Body))
			}
			var payload map[string]json.RawMessage
			decodeJSON(t, response.Body, &payload)
			if _, ok := payload["display_title"]; ok != legacy {
				t.Fatalf("legacy=%t payload=%s", legacy, payload)
			}
			if !legacy {
				var source struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(payload["source"], &source); err != nil {
					t.Fatal(err)
				}
				if source.Type != "anthropic_example" {
					t.Fatalf("source: %+v", source)
				}
			}
		}
	})
	first := create("Shared display name", "stable-demo")
	second := create("Shared display name", "stable-demo")
	if first.ID == second.ID || first.Source.Type != "custom" || !strings.HasPrefix(first.LatestVersionID, "skver_") {
		t.Fatalf("stable shapes: %+v %+v", first, second)
	}
	t.Run("reject last version deletion and legacy cascading deletion", func(t *testing.T) {
		for _, item := range []struct {
			path   string
			legacy bool
		}{
			{"/v1/skills/" + first.ID + "/versions/" + first.LatestVersionID, false},
			{"/v1/skills/" + first.ID, true},
		} {
			response := doSkillRequest(t, app, http.MethodDelete, item.path, nil, defaultTestKey, item.legacy, "")
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		}
	})
	t.Run("legacy title uniqueness includes formal skills", func(t *testing.T) {
		body, contentType := skillMultipartBody(t, "Shared display name", []skillUploadFile{{FieldName: "files[]", Filename: "legacy/SKILL.md", Content: "---\nname: legacy\n---\nTest"}})
		response := doSkillRequest(t, app, http.MethodPost, "/v1/skills", body, defaultTestKey, true, contentType)
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
	})
	t.Run("reject slug change", func(t *testing.T) {
		body, contentType := stableSkillBody(t, "", "another-name")
		response := doSkillRequest(t, app, http.MethodPost, "/v1/skills/"+first.ID+"/versions", body, defaultTestKey, false, contentType)
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
	})
	t.Run("latest aliases and archive paths", func(t *testing.T) {
		for _, version := range []string{"latest", first.LatestVersionID, strings.Replace(first.LatestVersionID, "skver_", "skill_version_", 1)} {
			response := doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+first.ID+"/versions/"+version, nil, defaultTestKey, false, "")
			if response.StatusCode != 200 {
				t.Fatalf("retrieve: %d %s", response.StatusCode, readAll(t, response.Body))
			}
			var payload map[string]json.RawMessage
			decodeJSON(t, response.Body, &payload)
			if _, ok := payload["directory"]; ok {
				t.Fatal("stable version exposes directory")
			}
			if _, ok := payload["version"]; ok {
				t.Fatal("stable version exposes epoch version")
			}
		}
		response := doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+first.ID+"/versions/latest/content", nil, defaultTestKey, false, "")
		data := []byte(readAll(t, response.Body))
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if len(archive.File) != 2 || archive.File[0].Name != "stable-demo/SKILL.md" || archive.File[1].Name != "stable-demo/references/help.txt" {
			t.Fatalf("archive paths: %+v", archive.File)
		}
	})
	t.Run("formal newest first and legacy oldest first with separate cursors", func(t *testing.T) {
		body, contentType := stableSkillBody(t, "", "stable-demo")
		response := doSkillRequest(t, app, http.MethodPost, "/v1/skills/"+first.ID+"/versions", body, defaultTestKey, false, contentType)
		if response.StatusCode != 200 {
			t.Fatalf("version: %d %s", response.StatusCode, readAll(t, response.Body))
		}
		var version struct {
			ID string `json:"id"`
		}
		decodeJSON(t, response.Body, &version)
		for _, legacy := range []bool{false, true} {
			response = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+first.ID+"/versions?limit=1", nil, defaultTestKey, legacy, "")
			var page struct {
				Data     []map[string]json.RawMessage `json:"data"`
				NextPage *string                      `json:"next_page"`
				HasMore  *bool                        `json:"has_more"`
			}
			decodeJSON(t, response.Body, &page)
			var id string
			if len(page.Data) != 1 || page.NextPage == nil {
				t.Fatalf("page: %+v", page)
			}
			if err := json.Unmarshal(page.Data[0]["id"], &id); err != nil {
				t.Fatal(err)
			}
			expected := version.ID
			if legacy {
				expected = first.LatestVersionID
			}
			if id != expected || (page.HasMore != nil) != legacy {
				t.Fatalf("legacy=%t page=%+v", legacy, page)
			}
			response = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+first.ID+"/versions?page="+url.QueryEscape(*page.NextPage), nil, defaultTestKey, !legacy, "")
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		}
	})
	t.Run("concurrent deletions retain one version", func(t *testing.T) {
		response := doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+first.ID+"/versions", nil, defaultTestKey, false, "")
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		decodeJSON(t, response.Body, &page)
		if len(page.Data) != 2 {
			t.Fatalf("versions before deletion: %+v", page)
		}
		responses := make(chan *http.Response, 2)
		for _, version := range page.Data {
			go func(id string) {
				responses <- doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+first.ID+"/versions/"+id, nil, defaultTestKey, false, "")
			}(version.ID)
		}
		statuses := map[int]int{}
		for range 2 {
			response := <-responses
			statuses[response.StatusCode]++
			response.Body.Close()
		}
		if statuses[200] != 1 || statuses[400] != 1 {
			t.Fatalf("concurrent delete statuses: %v", statuses)
		}
	})
	t.Run("empty legacy skill is hidden in formal catalog", func(t *testing.T) {
		response := doSkillRequest(t, app, http.MethodDelete, "/v1/skills/"+second.ID+"/versions/"+second.LatestVersionID, nil, defaultTestKey, true, "")
		if response.StatusCode != 200 {
			t.Fatalf("legacy last version: %d %s", response.StatusCode, readAll(t, response.Body))
		}
		response.Body.Close()
		response = doSkillRequest(t, app, http.MethodGet, "/v1/skills/"+second.ID, nil, defaultTestKey, false, "")
		assertError(t, response, http.StatusBadRequest, "invalid_request_error")
		response = doSkillRequest(t, app, http.MethodGet, "/v1/skills?source=custom&limit=1000", nil, defaultTestKey, false, "")
		var page struct {
			Data []stableSkillAPIResponse `json:"data"`
		}
		decodeJSON(t, response.Body, &page)
		for _, skill := range page.Data {
			if skill.ID == second.ID {
				t.Fatal("empty skill included")
			}
		}
	})
}
