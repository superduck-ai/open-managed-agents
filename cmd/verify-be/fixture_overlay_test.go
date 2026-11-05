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

func TestBaselineFixtureOverlayOnlyAppliesToPerformanceBaseline(t *testing.T) {
	env := newEnvironment(t.TempDir(), t.TempDir(), "fixture-overlay-test")
	for _, name := range []string{"chat.performance", "files.performance", "chat.roundtrip"} {
		if path, err := env.baselineFixtureOverlay(scenarios[name]); err != nil || path != "" {
			t.Fatalf("candidate %s unexpectedly used an overlay: %s, %v", name, path, err)
		}
	}
	env.backendCommit = "reference-commit"
	for _, name := range []string{"chat.roundtrip", "files.lifecycle"} {
		if path, err := env.baselineFixtureOverlay(scenarios[name]); err != nil || path != "" {
			t.Fatalf("%s unexpectedly used an overlay: %s, %v", name, path, err)
		}
	}
}

func TestBaselineFixtureOverlayPreservesCurrentSource(t *testing.T) {
	env := newEnvironment(t.TempDir(), t.TempDir(), "fixture-overlay-test")
	env.backendCommit = "reference-commit"
	for _, name := range []string{"admin_api_keys_mapper", "code_session_mapper", "session_mapper", "session_thread_mapper", "session_resource_file_mapper", "session_resource_mapper"} {
		for _, root := range []string{env.root, filepath.Join(env.directory, "backend-source")} {
			path := filepath.Join(root, "internal", "db", name+".sqlmap.gen.go")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			source := "package db\nfunc buildExisting() string { return \"reference\" }\n"
			if root == env.root {
				source = "package db\nfunc buildExisting() string { return \"candidate\" }\nfunc candidateBudgetMethod() {}\n"
			}
			source += "func buildSessionResourceMapperInsertDirectory() string { return \"directory\" }\n"
			if name == "session_resource_mapper" && root != env.root {
				source = "package db\nfunc buildSessionResourceMapperInsertDirectory() string { return \"reference\" }\n"
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
	if len(overlay.Replace) != 6 {
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

func TestBaselineDirectoryOverlayRejectsMissingInputs(t *testing.T) {
	for _, missing := range []string{"current path", "reference path", "current builder", "reference builder"} {
		t.Run(missing, func(t *testing.T) {
			directory := t.TempDir()
			current, reference := filepath.Join(directory, "current.go"), filepath.Join(directory, "reference.go")
			for _, path := range []string{current, reference} {
				if missing == "current path" && path == current || missing == "reference path" && path == reference {
					continue
				}
				source := "package db\nfunc buildSessionResourceMapperInsertDirectory() {}\n"
				if missing == "current builder" && path == current || missing == "reference builder" && path == reference {
					source = "package db\n"
				}
				if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := baselineMapperSource(current, reference, "buildSessionResourceMapperInsertDirectory"); err == nil {
				t.Fatal("missing directory fixture silently accepted")
			}
		})
	}
}

func TestBaselineDirectoryOverlayPreservesCurrentScannerAndOtherSQL(t *testing.T) {
	directory := t.TempDir()
	current, reference := filepath.Join(directory, "current.go"), filepath.Join(directory, "reference.go")
	for path, source := range map[string]string{
		current: `package db
func buildSessionResourceMapperInsertDirectory() string { return "current-directory" }
func scanSessionResourceMapperFindByExternalID() string { return "current-scanner" }
func buildSessionResourceMapperInsert() string { return "current-resource" }
`,
		reference: `package db
func buildSessionResourceMapperInsertDirectory() string { return "base-directory" }
func scanSessionResourceMapperFindByExternalID() string { return "old-scanner" }
func buildSessionResourceMapperInsert() string { return "old-resource" }
`,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	merged, err := baselineMapperSource(current, reference, "buildSessionResourceMapperInsertDirectory")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base-directory", "current-scanner", "current-resource"} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("missing %s: %s", want, merged)
		}
	}
	for _, unwanted := range []string{"current-directory", "old-scanner", "old-resource"} {
		if strings.Contains(string(merged), unwanted) {
			t.Fatalf("unexpected %s: %s", unwanted, merged)
		}
	}
}

func TestFilesBaselineOverlayUsesReferenceAPIKeySQLAndScanner(t *testing.T) {
	env := newEnvironment(t.TempDir(), t.TempDir(), "files-fixture-overlay-test")
	env.backendCommit = "reference-commit"
	current := filepath.Join(env.root, "internal", "db", "admin_api_keys_mapper.sqlmap.gen.go")
	reference := filepath.Join(env.directory, "backend-source", "internal", "db", filepath.Base(current))
	for path, source := range map[string]string{
		current: `package db
func buildAdminAPIKeyMapperFindActiveByKeyHash() string { return "candidate-runtime-column" }
func scanAdminAPIKeyMapperFindActiveByKeyHash() string { return "candidate-six-columns" }
func candidateRuntimeBinding() {}
`,
		reference: `package db
func buildAdminAPIKeyMapperFindActiveByKeyHash() string { return "baseline-five-columns" }
func scanAdminAPIKeyMapperFindActiveByKeyHash() string { return "baseline-five-columns" }
`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := env.baselineFixtureOverlay(scenarios["files.performance"])
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
	if len(overlay.Replace) != 1 {
		t.Fatalf("Files 基线不应覆盖其他夹具：%v", overlay.Replace)
	}
	merged, err := os.ReadFile(overlay.Replace[current])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(merged), "baseline-five-columns") != 2 || strings.Contains(string(merged), "candidate-runtime-column") || strings.Contains(string(merged), "candidate-six-columns") || !strings.Contains(string(merged), "candidateRuntimeBinding") {
		t.Fatalf("基线查询与扫描不匹配：%s", merged)
	}
	original, err := os.ReadFile(current)
	if err != nil || !strings.Contains(string(original), "candidate-runtime-column") {
		t.Fatalf("候选源码不应被改写：%v", err)
	}
}
