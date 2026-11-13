//go:build integration

package platformadmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/skills"
)

type systemSkillTestStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *systemSkillTestStore) Ensure(context.Context) error { return nil }
func (s *systemSkillTestStore) Name() string                 { return "system-skills-test" }
func (s *systemSkillTestStore) Upload(_ context.Context, key string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
	data, err := io.ReadAll(body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	return storage.UploadResult{}, err
}
func (s *systemSkillTestStore) Open(_ context.Context, key string, _ *storage.ByteRange) (storage.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return storage.Object{}, errors.New("missing object")
	}
	return storage.Object{Body: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data))}, nil
}
func (s *systemSkillTestStore) Delete(_ context.Context, key string, _ storage.DeleteOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}
func (s *systemSkillTestStore) Copy(context.Context, string, string) (storage.CopyResult, error) {
	return storage.CopyResult{}, errors.New("copy not used")
}
func (s *systemSkillTestStore) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.objects) }

type systemSkillTestResource struct {
	ID             string  `json:"id"`
	Revision       int64   `json:"revision"`
	Listed         bool    `json:"listed"`
	DefaultVersion *string `json:"default_version"`
}

func TestSystemSkillsAdministrationPostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := New(database, nil)
	actor, backup := uuid.NewV4().String(), uuid.NewV4().String()
	if err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "System skills fixture"})
		if err != nil {
			return err
		}
		for _, id := range []string{actor, backup} {
			if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: id, ExternalID: "user_" + id, OrganizationUUID: org.UUID, Email: id + "@example.com", Name: "Skill Administrator", Role: "admin"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{actor, backup} {
		if err := service.SetAdministrator(t.Context(), id, "fixture operator", "", true); err != nil {
			t.Fatal(err)
		}
	}
	store := &systemSkillTestStore{objects: make(map[string][]byte)}
	handler := skills.NewHandler(database, store, nil)
	routes := NewHandler(service, func(r *http.Request) (platformsession.Session, error) {
		cookie, err := r.Cookie("sessionKey")
		if err != nil || cookie.Value != actor {
			return platformsession.Session{}, ErrUnauthenticated
		}
		return platformsession.Session{UserUUID: actor, VerifiedEmail: actor + "@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true).Routes(handler.AdminRoutes())
	send := func(method, path string, body io.Reader, contentType string, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, body)
		request.AddCookie(&http.Cookie{Name: "sessionKey", Value: actor})
		request.Header.Set("Content-Type", contentType)
		if csrf {
			request.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken(actor))
		}
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, request)
		return response
	}
	archive := func(name, text string) []byte {
		t.Helper()
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		file, err := writer.Create(name + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("---\nname: " + name + "\ndescription: Isolated acceptance fixture\n---\n\n" + text)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	uploadTo := func(target string, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, err := form.CreateFormFile("files[]", "fixture.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := form.WriteField("skill_id", target); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		return send("POST", "/skills/", &body, form.FormDataContentType(), true)
	}
	upload := func(data []byte) *httptest.ResponseRecorder {
		return uploadTo("", data)
	}

	requireStatus := func(response *httptest.ResponseRecorder, status int) {
		t.Helper()
		if response.Code != status {
			t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
		}
	}
	get := func() systemSkillTestResource {
		t.Helper()
		response := send("GET", "/skills/fixture-system-skill", nil, "", false)
		requireStatus(response, 200)
		var result systemSkillTestResource
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	action := func(kind string) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(struct {
			Action string `json:"action"`
		}{kind})
		if err != nil {
			t.Fatal(err)
		}
		return send("POST", "/skills/fixture-system-skill/actions", bytes.NewReader(data), "application/json", true)
	}

	requireStatus(send("POST", "/skills/inspect", nil, "", false), 403)
	requireStatus(upload([]byte("invalid zip")), 400)
	if store.count() != 0 {
		t.Fatal("invalid import created storage object")
	}
	initial := archive("fixture-system-skill", "first version")
	inspect := func(target string, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var inspectionBody bytes.Buffer
		inspectionForm := multipart.NewWriter(&inspectionBody)
		inspectionFile, err := inspectionForm.CreateFormFile("files[]", "fixture.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := inspectionFile.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := inspectionForm.WriteField("skill_id", target); err != nil {
			t.Fatal(err)
		}
		if err := inspectionForm.Close(); err != nil {
			t.Fatal(err)
		}
		return send("POST", "/skills/inspect", &inspectionBody, inspectionForm.FormDataContentType(), true)
	}
	inspection := inspect("", initial)
	requireStatus(inspection, 200)
	var preview struct {
		Name  string   `json:"name"`
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(inspection.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Name != "fixture-system-skill" || len(preview.Files) != 1 || store.count() != 0 {
		t.Fatal("inspection must parse the package without storing it")
	}
	requireStatus(upload(initial), 200)
	skill := get()
	if skill.Listed || skill.DefaultVersion == nil {
		t.Fatalf("new import must set default and stay unlisted: %+v", skill)
	}
	firstVersion := *skill.DefaultVersion
	consoleRoutes := handler.ConsoleRoutes()
	consolePreview := func(version, query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/fixture-system-skill/versions/"+version+"/preview"+query, nil)
		request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
			CredentialType: auth.CredentialTypePlatformSession,
			UserUUID:       actor,
			WorkspaceUUID:  uuid.NewV4().String(),
		}))
		recorder := httptest.NewRecorder()
		consoleRoutes.ServeHTTP(recorder, request)
		return recorder
	}
	requireStatus(consolePreview(firstVersion, ""), 404)
	requireStatus(consolePreview(firstVersion, "?download=1"), 404)
	requireStatus(send("GET", "/skills/fixture-system-skill/versions/"+firstVersion+"/content", nil, "", false), 200)
	requireStatus(action("list"), 200)
	skill = get()
	if !skill.Listed || skill.DefaultVersion == nil || *skill.DefaultVersion != firstVersion {
		t.Fatal("list did not expose default version")
	}
	requireStatus(consolePreview(firstVersion, ""), 200)
	{
		objects := store.count()
		wrongName := archive("different-skill", "wrong target")
		requireStatus(inspect(skill.ID, wrongName), 200)
		response := uploadTo(skill.ID, wrongName)
		requireStatus(response, 400)
		var result struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Error.Code != "skill_name_mismatch" {
			t.Fatalf("name mismatch response: %s error=%v", response.Body.String(), err)
		}
		if store.count() != objects {
			t.Fatal("rejected name mismatch left side effects")
		}
	}

	versions, _, err := database.ListSystemSkillVersionsPage(t.Context(), db.ListSystemSkillVersionsPageParams{SkillExternalID: skill.ID, Limit: 20})
	if err != nil || len(versions) != 1 {
		t.Fatalf("legacy version feed: %v %+v", err, versions)
	}
	requireStatus(uploadTo(skill.ID, archive("fixture-system-skill", "second version")), 200)
	skill = get()
	if !skill.Listed || skill.DefaultVersion == nil || *skill.DefaultVersion == firstVersion {
		t.Fatalf("update did not switch active default while keeping listed: %+v", skill)
	}
	secondVersion := *skill.DefaultVersion
	resolved, err := database.GetSystemSkillVersion(t.Context(), skill.ID, "latest")
	if err != nil || resolved.Version != secondVersion {
		t.Fatalf("latest resolve: %+v %v", resolved, err)
	}
	requireStatus(action("unlist"), 200)
	requireStatus(consolePreview(secondVersion, ""), 404)
	requireStatus(consolePreview(secondVersion, "?download=1"), 404)
	count, err := database.CountListedSystemSkills(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("unlisted catalog count=%d error=%v", count, err)
	}
	resolved, err = database.GetSystemSkillVersion(t.Context(), skill.ID, "latest")
	if err != nil || resolved.Version != secondVersion {
		t.Fatal("unlisting broke existing reference")
	}

	requireStatus(action("list"), 200)
	requireStatus(consolePreview(secondVersion, ""), 200)
	requireStatus(consolePreview(secondVersion, "?download=1"), 200)
	preloadedCount, err := skills.SyncBundledSkills(t.Context(), database, store, catalog.Files)
	if err != nil || preloadedCount != 4 {
		t.Fatalf("startup preload: count=%d error=%v", preloadedCount, err)
	}
	preloaded, err := database.GetSystemSkill(t.Context(), "xlsx")
	if err != nil || !preloaded.Bundled || !preloaded.Listed {
		t.Fatalf("preloaded skill: %+v %v", preloaded, err)
	}
	requireStatus(send("DELETE", "/skills/xlsx", nil, "", true), 403)
	requireStatus(inspect("xlsx", archive("xlsx", "replace bundled")), 200)
	requireStatus(uploadTo("xlsx", archive("xlsx", "replace bundled")), 200)
	overridden, err := database.GetSystemSkill(t.Context(), "xlsx")
	if err != nil || *overridden.LatestVersion == *preloaded.LatestVersion {
		t.Fatalf("bundled override inactive: %+v %v", overridden, err)
	}
	requireStatus(send("DELETE", "/skills/xlsx", nil, "", false), 403)
	if _, err := skills.SyncBundledSkills(t.Context(), database, store, catalog.Files); err != nil {
		t.Fatal(err)
	}
	restarted, err := database.GetSystemSkill(t.Context(), "xlsx")
	if err != nil || *restarted.LatestVersion != *overridden.LatestVersion {
		t.Fatalf("startup clobbered uploaded override: %+v %v", restarted, err)
	}
	requireStatus(send("DELETE", "/skills/xlsx", nil, "", true), 204)
	restored, err := database.GetSystemSkill(t.Context(), "xlsx")
	if err != nil || *restored.LatestVersion != *preloaded.LatestVersion || !restored.Bundled {
		t.Fatalf("DELETE removed bundled skill instead of restoring: %+v %v", restored, err)
	}
	requireStatus(send("DELETE", "/skills/xlsx", nil, "", true), 403)
	for _, id := range []string{"xlsx", "pptx", "pdf", "docx"} {
		loaded, err := database.GetSystemSkill(t.Context(), id)
		if err != nil || loaded.LatestVersion == nil || !loaded.Bundled {
			t.Fatalf("missing bundled skill %s: %v", id, err)
		}
		requireStatus(send("GET", "/skills/"+id+"/versions/"+*loaded.LatestVersion+"/content?download=1", nil, "", false), 200)
	}
	requireStatus(upload(archive("delete-me", "uploaded skill")), 200)
	beforeDelete := store.count()
	requireStatus(send("DELETE", "/skills/delete-me", nil, "", false), 403)
	requireStatus(send("DELETE", "/skills/delete-me", nil, "", true), 204)
	requireStatus(send("GET", "/skills/delete-me", nil, "", false), 404)
	if store.count() != beforeDelete {
		t.Fatal("catalog deletion removed historical archive")
	}
	if err := service.SetAdministrator(t.Context(), actor, "fixture operator", "", false); err != nil {
		t.Fatal(err)
	}
	requireStatus(send("GET", "/skills/", nil, "", false), 403)
	staleAuthorization := NewHandler(service, func(*http.Request) (platformsession.Session, error) {
		return platformsession.Session{UserUUID: actor, VerifiedEmail: actor + "@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true)
	staleAuthorization.verify = func(context.Context, string) (db.PlatformAdminAccount, error) {
		return db.PlatformAdminAccount{UUID: actor, Granted: true}, nil
	}
	request := httptest.NewRequest("POST", "/skills/fixture-system-skill/actions", bytes.NewBufferString(`{"action":"unlist"}`))
	request.AddCookie(&http.Cookie{Name: "sessionKey", Value: actor})
	request.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken(actor))
	response := httptest.NewRecorder()
	staleAuthorization.Routes(handler.AdminRoutes()).ServeHTTP(response, request)
	requireStatus(response, 403)
}
