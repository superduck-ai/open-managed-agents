package evaluations

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestAssignStoredCompanionsMatchesNamesAndCaseDirectories(t *testing.T) {
	cases := []normalizedCase{{
		Name:       "alpha",
		InputFiles: json.RawMessage(`[{"name":"brief.txt","file_id":"file_old"},{"name":"keep.txt","file_id":"file_keep"}]`),
	}, {
		Name: "beta",
	}}
	stored := []storedCompanion{
		{kind: "input", rel: "brief.txt", ref: sessionCaseFileRef{Name: "brief.txt", FileID: "file_brief"}},
		{kind: "input", rel: "alpha/notes.txt", ref: sessionCaseFileRef{Name: "alpha/notes.txt", FileID: "file_notes"}},
		{kind: "reference", rel: "gold.txt", ref: sessionCaseFileRef{Name: "gold.txt", FileID: "file_gold"}},
	}
	assigned, err := assignStoredCompanions(cases, stored)
	if err != nil {
		t.Fatal(err)
	}
	alphaInput := string(assigned[0].InputFiles)
	if !strings.Contains(alphaInput, `"file_id":"file_brief"`) || strings.Contains(alphaInput, `"file_id":"file_old"`) || !strings.Contains(alphaInput, `"file_id":"file_keep"`) || !strings.Contains(alphaInput, `"file_id":"file_notes"`) {
		t.Fatalf("alpha input = %s", alphaInput)
	}
	if strings.Contains(string(assigned[1].InputFiles), "file_notes") || strings.Contains(string(assigned[1].InputFiles), "file_brief") {
		t.Fatalf("beta input = %s", assigned[1].InputFiles)
	}
	if !strings.Contains(string(assigned[0].ReferenceFiles), `"file_id":"file_gold"`) || !strings.Contains(string(assigned[1].ReferenceFiles), `"file_id":"file_gold"`) {
		t.Fatalf("references = %s %s", assigned[0].ReferenceFiles, assigned[1].ReferenceFiles)
	}
	paths, err := assignStoredCompanions([]normalizedCase{{
		Name:           "alpha",
		InputFiles:     json.RawMessage(`["brief.txt"]`),
		ReferenceFiles: json.RawMessage(`["gold.txt"]`),
	}}, stored)
	if err != nil || !strings.Contains(string(paths[0].InputFiles), `"file_id":"file_brief"`) || !strings.Contains(string(paths[0].ReferenceFiles), `"file_id":"file_gold"`) {
		t.Fatalf("paths = %v %s %s", err, paths[0].InputFiles, paths[0].ReferenceFiles)
	}
	_, err = assignStoredCompanions([]normalizedCase{{
		Name:       "alpha",
		InputFiles: json.RawMessage(`[{"name":"missing.txt"}]`),
	}}, stored)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.PublicMessage != importCompanionUnresolvedMessage {
		t.Fatalf("unresolved = %v", err)
	}
}
