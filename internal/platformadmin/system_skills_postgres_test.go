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
	"strconv"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
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
			if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: id, ExternalID: "user_" + id, OrganizationUUID: org.UUID, Email: id + "@example.com", Role: "admin"}); err != nil {
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
	uploadTo := func(target, version string, revision int64, publish bool, data []byte) *httptest.ResponseRecorder {
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
		for key, value := range map[string]string{"skill_id": target, "version": version, "revision": strconv.FormatInt(revision, 10), "publish": strconv.FormatBool(publish)} {
			if err := form.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		return send("POST", "/skills/", &body, form.FormDataContentType(), true)
	}
	upload := func(version string, revision int64, publish bool, data []byte) *httptest.ResponseRecorder {
		return uploadTo("", version, revision, publish, data)
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
	action := func(kind, version string, revision int64) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(struct {
			Action   string `json:"action"`
			Version  string `json:"version"`
			Revision int64  `json:"revision"`
		}{kind, version, revision})
		if err != nil {
			t.Fatal(err)
		}
		return send("POST", "/skills/fixture-system-skill/actions", bytes.NewReader(data), "application/json", true)
	}

	requireStatus(send("POST", "/skills/inspect", nil, "", false), 403)
	requireStatus(upload("1.0.0", 0, false, []byte("invalid zip")), 400)
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
	requireStatus(upload("1.0.0", 0, false, initial), 200)
	skill := get()
	if skill.DefaultVersion != nil || skill.Listed {
		t.Fatal("draft became visible")
	}
	if _, err := database.GetBuiltinSkill(t.Context(), skill.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("draft leaked to public lookup: %v", err)
	}
	if _, err := database.GetBuiltinSkillVersion(t.Context(), skill.ID, "1.0.0"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("draft version leaked: %v", err)
	}
	requireStatus(send("GET", "/skills/fixture-system-skill/versions/1.0.0/content", nil, "", false), 200)
	requireStatus(upload("1.0.0", skill.Revision, false, archive("fixture-system-skill", "changed content")), 409)
	requireStatus(upload("1.0.0", 0, false, initial), 200)
	if store.count() != 1 {
		t.Fatalf("failed/duplicate uploads leaked: %d", store.count())
	}
	requireStatus(action("publish", "1.0.0", skill.Revision-1), 409)
	requireStatus(action("publish", "1.0.0", skill.Revision), 200)
	skill = get()
	if !skill.Listed || skill.DefaultVersion == nil || *skill.DefaultVersion != "1.0.0" {
		t.Fatal("first publish did not set default and list")
	}
	{
		before := send("GET", "/skills/fixture-system-skill/events", nil, "", false).Body.String()
		requireStatus(action("publish", "missing", skill.Revision), 404)
		requireStatus(action("publish", "1.0.0", skill.Revision), 200)
		if got := get(); got.Revision != skill.Revision || !got.Listed || *got.DefaultVersion != *skill.DefaultVersion {
			t.Errorf("repeated publish changed skill: before=%+v after=%+v", skill, got)
		}
		if after := send("GET", "/skills/fixture-system-skill/events", nil, "", false).Body.String(); after != before {
			t.Error("repeated publish created audit events")
		}
	}
	skill = get()
	{
		before := send("GET", "/skills/fixture-system-skill/events", nil, "", false).Body.String()
		objects := store.count()
		wrongName := archive("different-skill", "wrong target")
		requireStatus(inspect(skill.ID, wrongName), 200)
		response := uploadTo(skill.ID, "9.0.0", skill.Revision, false, wrongName)
		requireStatus(response, 400)
		var result struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Error.Code != "skill_name_mismatch" {
			t.Fatalf("name mismatch response: %s error=%v", response.Body.String(), err)
		}
		if get().Revision != skill.Revision || store.count() != objects || send("GET", "/skills/fixture-system-skill/events", nil, "", false).Body.String() != before {
			t.Fatal("rejected name mismatch left side effects")
		}
	}

	versions, _, err := database.ListBuiltinSkillVersionsPage(t.Context(), db.ListBuiltinSkillVersionsPageParams{SkillExternalID: skill.ID, Limit: 20})
	if err != nil || len(versions) != 1 {
		t.Fatalf("legacy version feed: %v %+v", err, versions)
	}
	requireStatus(upload("2.0.0", skill.Revision, false, archive("fixture-system-skill", "second version")), 200)
	skill = get()
	requireStatus(action("set_default", "2.0.0", skill.Revision), 409)
	requireStatus(action("publish", "2.0.0", skill.Revision), 200)
	skill = get()
	if *skill.DefaultVersion != "1.0.0" {
		t.Fatal("new publish changed existing default")
	}
	requireStatus(action("set_default", "2.0.0", skill.Revision), 200)
	skill = get()
	resolved, err := database.GetBuiltinSkillVersion(t.Context(), skill.ID, "latest")
	if err != nil || resolved.Version != "2.0.0" {
		t.Fatalf("latest resolve: %+v %v", resolved, err)
	}
	requireStatus(action("unlist", "", skill.Revision), 200)
	count, err := database.CountBuiltinSkills(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("unlisted catalog count=%d error=%v", count, err)
	}
	resolved, err = database.GetBuiltinSkillVersion(t.Context(), skill.ID, "latest")
	if err != nil || resolved.Version != "2.0.0" {
		t.Fatal("unlisting broke existing reference")
	}
	{
		before := get()
		events := send("GET", "/skills/fixture-system-skill/events", nil, "", false).Body.String()
		requireStatus(action("publish", "1.0.0", before.Revision), 200)
		after := get()
		if after.Listed || after.Revision != before.Revision || *after.DefaultVersion != *before.DefaultVersion || events != send("GET", "/skills/fixture-system-skill/events", nil, "", false).Body.String() {
			t.Fatal("repeated publish changed unlisted skill")
		}
	}

	requireStatus(action("list", "", get().Revision), 200)
	skill = get()
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, version := range []string{"3.0.0", "4.0.0"} {
		wg.Go(func() { statuses <- upload(version, skill.Revision, false, initial).Code })
	}
	wg.Wait()
	close(statuses)
	successes, conflicts := 0, 0
	for status := range statuses {
		if status == 200 {
			successes++
		}
		if status == 409 {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent writes: success=%d conflict=%d", successes, conflicts)
	}
	if store.count() != 3 {
		t.Fatalf("concurrent rejection leaked object: %d", store.count())
	}
	requireStatus(upload("5.0.0", get().Revision, true, archive("fixture-system-skill", "published import")), 200)
	if got := get(); *got.DefaultVersion != "2.0.0" {
		t.Fatal("published import changed existing default")
	}
	requireStatus(uploadTo("", "1.0.0", 0, true, archive("immediate-publish", "first published import")), 200)
	imported, err := database.GetSystemSkill(t.Context(), "immediate-publish")
	if err != nil || !imported.Listed || imported.LatestVersion == nil || *imported.LatestVersion != "1.0.0" {
		t.Fatalf("first published import: %+v %v", imported, err)
	}
	events, err := database.SystemSkillEvents(t.Context(), imported.UUID, 10, 0)
	if err != nil || len(events) != 2 || events[0].Action != "publish" || events[1].Action != "import_draft" {
		t.Fatalf("published import audit order: %+v %v", events, err)
	}

	requireStatus(upload("6.0.0", get().Revision, false, archive("fixture-system-skill", "seed collision")), 200)
	existing, err := database.GetSystemSkill(t.Context(), "fixture-system-skill")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := database.SystemSkillVersion(t.Context(), existing.UUID, "6.0.0")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = database.UpsertBuiltinSkillWithVersion(t.Context(), db.BuiltinSkill{ExternalID: existing.ExternalID, DisplayTitle: existing.DisplayTitle}, db.BuiltinSkillVersion{
		ExternalID: draft.ExternalID, Version: draft.Version, Name: draft.Name, Description: draft.Description, Directory: draft.Directory, S3Bucket: draft.S3Bucket, S3Key: draft.S3Key, SizeBytes: draft.SizeBytes, SHA256: draft.SHA256, CreatedAt: draft.CreatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := database.GetBuiltinSkillVersion(t.Context(), existing.ExternalID, "latest")
	if err != nil || seeded.Version != "6.0.0" {
		t.Fatalf("seeded draft must resolve: %+v %v", seeded, err)
	}
	if _, err := database.SoftDeleteMissingBuiltinSkills(t.Context(), []string{existing.ExternalID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	objects := store.count()
	requireStatus(uploadTo("", "2.0.0", 0, true, archive("immediate-publish", "pruned name")), 409)
	if store.count() != objects {
		t.Fatal("pruned name rejection leaked archive")
	}

	requireStatus(send("GET", "/skills/fixture-system-skill/events", nil, "", false), 200)
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
	request := httptest.NewRequest("POST", "/skills/fixture-system-skill/actions", bytes.NewBufferString(`{"action":"unlist","revision":1}`))
	request.AddCookie(&http.Cookie{Name: "sessionKey", Value: actor})
	request.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken(actor))
	response := httptest.NewRecorder()
	staleAuthorization.Routes(handler.AdminRoutes()).ServeHTTP(response, request)
	requireStatus(response, 403)
}
