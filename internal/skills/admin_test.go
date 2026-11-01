package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSystemSkillVersionValidation(t *testing.T) {
	for _, value := range []string{"", "latest", "..", "../other", ".hidden", "a/b", "中文", strings.Repeat("a", 65)} {
		if validSystemVersion(value, false) {
			t.Errorf("accepted invalid version %q", value)
		}
	}
	for _, value := range []string{"1", "1.2.0", "20261101", "v2-beta_1"} {
		if !validSystemVersion(value, false) {
			t.Errorf("rejected version %q", value)
		}
	}
}

func TestSystemSkillArchiveRejectsSymlinksAndTraversal(t *testing.T) {
	for _, entry := range []string{"demo/link", "../SKILL.md"} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		header := &zip.FileHeader{Name: entry, Method: zip.Deflate}
		if entry == "demo/link" {
			header.SetMode(os.ModeSymlink | 0777)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("target")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := skillPackageFromArchive(buffer.Bytes(), MaxSkillPackageBytes, true); err == nil {
			t.Errorf("accepted unsafe archive %q", entry)
		}
	}
}

func TestSystemSkillErrorsPreservePublicBoundary(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{db.ErrSystemSkillDenied, 403, "platform_access_denied"},
		{db.ErrNotFound, 404, "not_found"},
		{db.ErrSystemSkillRevision, 409, "revision_conflict"},
		{db.ErrVersionConflict, 409, "version_conflict"},
		{db.ErrInvalidState, 409, "state_conflict"},
		{db.ErrSystemSkillNameMismatch, 400, "skill_name_mismatch"},
		{packageError{Status: 413, Message: "too large"}, 413, "invalid_package"},
		{errors.New("private credential detail"), 503, "unavailable"},
	} {
		e := systemSkillError(tc.err)
		if e.Status != tc.status || e.Code != tc.code || strings.Contains(e.Message, "private") {
			t.Errorf("error mapping: %+v", e)
		}
	}
}

func TestSystemSkillRoutesRejectMissingPlatformIdentity(t *testing.T) {
	routes := NewHandler(nil, nil, nil).AdminRoutes()
	for _, principal := range []auth.Principal{
		{},
		{CredentialType: auth.CredentialTypeAPIKey, UserUUID: "account"},
		{CredentialType: auth.CredentialTypePlatformSession},
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/", nil)
			request = request.WithContext(auth.WithPrincipal(request.Context(), principal))
			routes.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d %s", principal.CredentialType, method, response.Code, response.Body.String())
			}
		}
	}
}
