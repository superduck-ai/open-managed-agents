package llmproviders

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestValidateSlugRejectsUnsafeValues(t *testing.T) {
	for _, slug := range []string{"", "Default", "oma/kimi", "-oma", ".oma", "o ma", strings.Repeat("a", 33)} {
		if err := ValidateSlug(slug); !errors.Is(err, ErrInvalidSlug) {
			t.Fatalf("ValidateSlug(%q) error = %v, want ErrInvalidSlug", slug, err)
		}
	}
	for _, slug := range []string{"oma", "default-2", "a", "dash.scope_cn", strings.Repeat("a", 32)} {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("ValidateSlug(%q) error = %v", slug, err)
		}
	}
}

func TestParseModelRefSplitsOnlyAtFirstSlash(t *testing.T) {
	for _, modelID := range []string{"kimi-k2.5", "/kimi-k2.5", "oma/"} {
		if _, _, ok := ParseModelRef(modelID); ok {
			t.Fatalf("ParseModelRef(%q) succeeded", modelID)
		}
	}
	slug, upstreamModelID, ok := ParseModelRef("oma/deepseek-ai/deepseek-v4.1-flash")
	if !ok || slug != "oma" || upstreamModelID != "deepseek-ai/deepseek-v4.1-flash" {
		t.Fatalf("ParseModelRef() = (%q, %q, %t)", slug, upstreamModelID, ok)
	}
	if QualifiedModelID(slug, upstreamModelID) != "oma/deepseek-ai/deepseek-v4.1-flash" {
		t.Fatal("QualifiedModelID() did not round trip")
	}
}

func TestNextDefaultSlugFillsFirstFreeSuffix(t *testing.T) {
	tests := []struct {
		taken []string
		want  string
	}{
		{nil, "default"},
		{[]string{"oma"}, "default"},
		{[]string{"default"}, "default-2"},
		{[]string{"default", "default-2", "default-4"}, "default-3"},
	}
	for _, test := range tests {
		if got := NextDefaultSlug(test.taken); got != test.want {
			t.Fatalf("NextDefaultSlug(%v) = %q, want %q", test.taken, got, test.want)
		}
	}
}

func TestCatalogLookupFailures(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{
		{Slug: "oma", ModelIDs: []string{"kimi-k2.5"}},
		{Slug: "default", ModelIDs: []string{"kimi-k2.5"}},
	})
	for _, modelID := range []string{"glm-5", "missing/kimi-k2.5", "oma/glm-5"} {
		if _, err := catalog.Lookup(modelID); !errors.Is(err, ErrModelNotConfigured) {
			t.Fatalf("Lookup(%q) error = %v, want ErrModelNotConfigured", modelID, err)
		}
	}
	_, err := catalog.Lookup("kimi-k2.5")
	var ambiguous *AmbiguousModelError
	if !errors.Is(err, ErrAmbiguousModel) || !errors.As(err, &ambiguous) ||
		!slices.Equal(ambiguous.Candidates, []string{"oma/kimi-k2.5", "default/kimi-k2.5"}) {
		t.Fatalf("Lookup(kimi-k2.5) error = %v", err)
	}
}

func TestCatalogLookupResolvesQualifiedAndLegacyModelIDs(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{
		{Slug: "oma", ModelIDs: []string{"kimi-k2.5", "deepseek-ai/deepseek-v4.1-flash"}},
		{Slug: "default", ModelIDs: []string{"kimi-k2.5", "glm-5"}},
		{Slug: "deepseek-ai", ModelIDs: []string{"deepseek-v4.1-flash"}},
	})
	tests := []struct {
		modelID      string
		wantSlug     string
		wantUpstream string
	}{
		{"oma/kimi-k2.5", "oma", "kimi-k2.5"},
		{"default/kimi-k2.5", "default", "kimi-k2.5"},
		{"oma/deepseek-ai/deepseek-v4.1-flash", "oma", "deepseek-ai/deepseek-v4.1-flash"},
		{"deepseek-ai/deepseek-v4.1-flash", "deepseek-ai", "deepseek-v4.1-flash"},
		{"glm-5", "default", "glm-5"},
	}
	for _, test := range tests {
		selected, err := catalog.Lookup(test.modelID)
		if err != nil || selected.Provider.Slug != test.wantSlug || selected.UpstreamModelID != test.wantUpstream {
			t.Fatalf("Lookup(%q) = (%+v, %v)", test.modelID, selected, err)
		}
	}
	want := []string{
		"oma/kimi-k2.5",
		"oma/deepseek-ai/deepseek-v4.1-flash",
		"default/kimi-k2.5",
		"default/glm-5",
		"deepseek-ai/deepseek-v4.1-flash",
	}
	if got := catalog.ModelIDs(); !slices.Equal(got, want) {
		t.Fatalf("ModelIDs() = %#v, want %#v", got, want)
	}
}

func TestReplaceRequestModelRejectsInvalidBodies(t *testing.T) {
	for _, body := range []string{`[]`, `{"model":"a","model":"b"}`, `{"max_tokens":1}`, `{"model":""}`} {
		if _, err := ReplaceRequestModel([]byte(body), "kimi-k2.5"); err == nil {
			t.Fatalf("ReplaceRequestModel(%s) succeeded", body)
		}
	}
}

func TestReplaceRequestModelRewritesOnlyTopLevelModel(t *testing.T) {
	body := []byte(`{"metadata":{"model":"oma/kimi-k2.5"},"model" :  "oma/deepseek-ai/deepseek-v4.1-flash" ,"stream":true}`)
	replaced, err := ReplaceRequestModel(body, "deepseek-ai/deepseek-v4.1-flash")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"metadata":{"model":"oma/kimi-k2.5"},"model" :  "deepseek-ai/deepseek-v4.1-flash" ,"stream":true}`
	if string(replaced) != want {
		t.Fatalf("ReplaceRequestModel() = %s", replaced)
	}
	if string(body) != `{"metadata":{"model":"oma/kimi-k2.5"},"model" :  "oma/deepseek-ai/deepseek-v4.1-flash" ,"stream":true}` {
		t.Fatal("ReplaceRequestModel() mutated the original body")
	}
	unchanged := []byte(`{"model":"kimi-k2.5"}`)
	if same, err := ReplaceRequestModel(unchanged, "kimi-k2.5"); err != nil || string(same) != string(unchanged) {
		t.Fatalf("ReplaceRequestModel() unchanged = (%s, %v)", same, err)
	}
	escaped, err := ReplaceRequestModel([]byte("{\"model\":\"oma/a\\u002fb\"}"), "a/b")
	if err != nil || string(escaped) != `{"model":"a/b"}` {
		t.Fatalf("ReplaceRequestModel() escaped = (%s, %v)", escaped, err)
	}
}
