package skills

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
	if !input.Ready || input.Publish || !strings.HasPrefix(input.Version.Version, "auto-") || input.Skill.IconKey != "file-text" {
		t.Fatalf("wrong automatic import: %+v", input)
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
