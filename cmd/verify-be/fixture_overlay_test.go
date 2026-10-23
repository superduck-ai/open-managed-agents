package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaselineFixtureOverlayRejectsMissingReferenceMapper(t *testing.T) {
	env := newEnvironment(t.TempDir(), t.TempDir(), "fixture-overlay-test")
	env.backendCommit = "reference-commit"
	if _, err := env.baselineFixtureOverlay(scenarios["chat.performance"]); err == nil {
		t.Fatal("missing reference SQL silently fell back to candidate fixtures")
	}
}

func TestBaselineFixtureOverlayOnlyAppliesToChatBaseline(t *testing.T) {
	env := newEnvironment(t.TempDir(), t.TempDir(), "fixture-overlay-test")
	for _, name := range []string{"chat.performance", "files.performance", "chat.roundtrip"} {
		if path, err := env.baselineFixtureOverlay(scenarios[name]); err != nil || path != "" {
			t.Fatalf("candidate %s unexpectedly used an overlay: %s, %v", name, path, err)
		}
	}
	env.backendCommit = "reference-commit"
	for _, name := range []string{"files.performance", "chat.roundtrip"} {
		if path, err := env.baselineFixtureOverlay(scenarios[name]); err != nil || path != "" {
			t.Fatalf("%s unexpectedly used an overlay: %s, %v", name, path, err)
		}
	}
}

func TestBaselineFixtureOverlayPreservesCurrentSource(t *testing.T) {
	env := newEnvironment(t.TempDir(), t.TempDir(), "fixture-overlay-test")
	env.backendCommit = "reference-commit"
	for _, name := range []string{"session_mapper", "session_thread_mapper", "session_resource_file_mapper"} {
		for _, root := range []string{env.root, filepath.Join(env.directory, "backend-source")} {
			path := filepath.Join(root, "internal", "db", name+".sqlmap.gen.go")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			source := "package db\nfunc buildExisting() string { return \"reference\" }\n"
			if root == env.root {
				source = "package db\nfunc buildExisting() string { return \"candidate\" }\nfunc candidateBudgetMethod() {}\n"
			}
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	path, err := env.baselineFixtureOverlay(scenarios["chat.performance"])
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var overlay fixtureOverlay
	if err := json.Unmarshal(data, &overlay); err != nil {
		t.Fatal(err)
	}
	if len(overlay.Replace) != 3 {
		t.Fatalf("unexpected fixture scope: %v", overlay.Replace)
	}
	if _, exists := overlay.Replace[filepath.Join(env.root, "internal", "db", "llm_provider_mapper.sqlmap.gen.go")]; exists {
		t.Fatal("Provider setup must use the public API instead of a baseline database interface")
	}
	for current, reference := range overlay.Replace {
		before, err := os.ReadFile(current)
		if err != nil || !strings.Contains(string(before), `return "candidate"`) {
			t.Fatalf("current mapper changed: %v", err)
		}
		expected := filepath.Join(env.directory, "fixture-"+filepath.Base(current)+".overlay")
		if reference != expected {
			t.Fatalf("mapper reference = %s, want %s", reference, expected)
		}
		merged, err := os.ReadFile(reference)
		if err != nil || !strings.Contains(string(merged), `return "reference"`) || !strings.Contains(string(merged), "func candidateBudgetMethod()") {
			t.Fatalf("基线 SQL 或当前接口实现丢失：%s %v", merged, err)
		}
	}
}
