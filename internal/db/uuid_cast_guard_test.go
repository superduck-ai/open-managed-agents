package db

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var forbiddenUUIDBoundaryPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{
		name:    "UUID input parameter cast",
		pattern: regexp.MustCompile(`(?i)cast\(\s*:[a-z0-9_]*(?:uuid|_uuid)\s+as\s+uuid\s*\)`),
	},
	{
		name:    "UUID output column cast",
		pattern: regexp.MustCompile(`(?i)cast\(\s*[a-z0-9_.]*(?:uuid|_uuid)\s+as\s+text\s*\)\s+as\s+[a-z0-9_]*(?:uuid|_uuid)`),
	},
	{
		name:    "UUID identifier column cast",
		pattern: regexp.MustCompile(`(?i)cast\(\s*[a-z0-9_.]*(?:uuid|_uuid)\s+as\s+text\s*\)\s*=\s*:[a-z0-9_]+`),
	},
}

func TestForbiddenUUIDBoundaryPatternsMatchMultilineSQL(t *testing.T) {
	samples := []string{
		"CAST(\n :workspace_uuid\n AS uuid\n)",
		"CAST(\n workspaces.uuid\n AS text\n) AS workspace_uuid",
		"CAST(\n workspaces.uuid\n AS text\n) = :identifier",
	}
	if len(samples) != len(forbiddenUUIDBoundaryPatterns) {
		t.Fatalf("sample count = %d, pattern count = %d", len(samples), len(forbiddenUUIDBoundaryPatterns))
	}
	for index, forbidden := range forbiddenUUIDBoundaryPatterns {
		if !forbidden.pattern.MatchString(samples[index]) {
			t.Errorf("%s did not match multiline SQL %q", forbidden.name, samples[index])
		}
	}
}

func TestProductionSQLKeepsTypedUUIDBoundary(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate UUID cast guard source")
	}
	directory := filepath.Dir(currentFile)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read db package: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for _, forbidden := range forbiddenUUIDBoundaryPatterns {
			for _, location := range forbidden.pattern.FindAllIndex(contents, -1) {
				lineNumber := bytes.Count(contents[:location[0]], []byte{'\n'}) + 1
				matchedSQL := strings.Join(strings.Fields(string(contents[location[0]:location[1]])), " ")
				t.Errorf(
					"%s:%d contains forbidden %s: %s",
					entry.Name(),
					lineNumber,
					forbidden.name,
					matchedSQL,
				)
			}
		}
	}
}
