package skills

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestSkillIconValidation(t *testing.T) {
	for _, key := range []string{"../image", "<svg>", "unknown", "BLOCKS"} {
		if _, err := skillIconKey(key); err == nil {
			t.Fatalf("accepted %q", key)
		}
	}
	for _, key := range skillIconKeys {
		if result, err := skillIconKey(key); err != nil || result != key {
			t.Fatalf("rejected %s: %v", key, err)
		}
	}
	if key, err := skillIconKey(""); err != nil || key != "blocks" {
		t.Fatal("missing default icon")
	}
}

func TestAutomaticSystemImportConfiguration(t *testing.T) {
	values := url.Values{"display_title": {"Demo"}, "icon_key": {"file-text"}}
	request := httptest.NewRequest("POST", "/", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	input, err := systemImportInput(request, skillPackage{Name: "demo", SHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(input.Version.Version, "auto-") || input.Skill.IconKey != "file-text" {
		t.Fatalf("wrong automatic import: %+v", input)
	}
}

func TestSystemImportOptionalIcon(t *testing.T) {
	for _, tc := range []struct {
		name string
		icon *string
		want string
	}{
		{name: "omitted"},
		{name: "empty", icon: new("")},
		{name: "explicit default", icon: new("blocks"), want: "blocks"},
		{name: "explicit icon", icon: new("file-text"), want: "file-text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := url.Values{"skill_id": {"demo"}, "revision": {"3"}}
			if tc.icon != nil {
				values.Set("icon_key", *tc.icon)
			}
			request := httptest.NewRequest("POST", "/", strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			input, err := systemImportInput(request, skillPackage{Name: "demo"})
			if err != nil {
				t.Fatal(err)
			}
			if input.Skill.IconKey != tc.want || input.ExpectedRevision != 3 {
				t.Fatalf("optional icon = %q, want %q; input=%+v", input.Skill.IconKey, tc.want, input)
			}
		})
	}
}

func TestConsoleSkillRoutesRejectUnscopedIdentity(t *testing.T) {
	routes := NewHandler(nil, nil, nil).ConsoleRoutes()
	for _, path := range []string{"/", "/inspect", "/skill_custom/icon"} {
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
		if response.Code != 403 {
			t.Fatalf("unscoped request %s: %d", path, response.Code)
		}
	}
}

func TestChangeSystemIconValidation(t *testing.T) {
	routes := NewHandler(nil, nil, nil).AdminRoutes()
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{name: "unknown json field", body: `{"icon_key":"blocks","revision":1,"extra":true}`, message: "图标参数无效"},
		{name: "zero revision", body: `{"icon_key":"blocks","revision":0}`, message: "修订号无效"},
		{name: "negative revision", body: `{"icon_key":"blocks","revision":-2}`, message: "修订号无效"},
		{name: "empty icon key", body: `{"icon_key":"","revision":1}`, message: "请选择有效的技能图标"},
		{name: "blank icon key", body: `{"icon_key":"   ","revision":1}`, message: "请选择有效的技能图标"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/demo/icon", bytes.NewBufferString(tc.body))
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
				CredentialType: auth.CredentialTypePlatformSession,
				UserUUID:       "admin-user",
			}))
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("invalid json error response: %v (%s)", err, rec.Body.String())
			}
			if payload.Error.Message != tc.message {
				t.Fatalf("message = %q, want %q", payload.Error.Message, tc.message)
			}
		})
	}
}

func TestConsoleSkillValidationAndErrorContract(t *testing.T) {
	routes := NewHandler(nil, nil, nil).ConsoleRoutes()
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		message string
	}{
		{name: "icon unknown field", method: http.MethodPatch, path: "/skill_custom/icon", body: `{"icon_key":"blocks","extra":true}`, message: "Invalid icon request payload"},
		{name: "icon empty key", method: http.MethodPatch, path: "/skill_custom/icon", body: `{"icon_key":""}`, message: "Please choose a valid skill icon"},
		{name: "icon blank key", method: http.MethodPatch, path: "/skill_custom/icon", body: `{"icon_key":"   "}`, message: "Please choose a valid skill icon"},
		{name: "catalog invalid source", method: http.MethodGet, path: "/?source=unknown", message: "Invalid skill filter parameters"},
		{name: "catalog invalid sort", method: http.MethodGet, path: "/?sort=popular", message: "Invalid skill filter parameters"},
		{name: "catalog limit zero", method: http.MethodGet, path: "/?limit=0", message: "Limit must be between 1 and 100"},
		{name: "catalog limit over max", method: http.MethodGet, path: "/?limit=101", message: "Limit must be between 1 and 100"},
		{name: "catalog negative offset", method: http.MethodGet, path: "/?offset=-1", message: "Offset is invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
				CredentialType: auth.CredentialTypePlatformSession,
				WorkspaceUUID:  "ws-1",
			}))
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("invalid json error response: %v (%s)", err, rec.Body.String())
			}
			if payload.Error.Code != "invalid_request" || payload.Error.Message != tc.message {
				t.Fatalf("error = %+v, want invalid_request / %q", payload.Error, tc.message)
			}
		})
	}
}
