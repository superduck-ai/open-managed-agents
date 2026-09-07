package sessioncreation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestValidateResources(t *testing.T) {
	t.Run("rejects too many files", func(t *testing.T) {
		resources := make([]Resource, 0, db.MaxSessionFileResources+1)
		for index := 0; index <= db.MaxSessionFileResources; index++ {
			resources = append(resources, testFileResource(t, "/workspace/files/"+strings.Repeat("x", index+1)))
		}
		if err := ValidateResources(resources); err == nil {
			t.Fatalf("ValidateResources() accepted more than %d files", db.MaxSessionFileResources)
		}
	})
	t.Run("rejects duplicate paths", func(t *testing.T) {
		resources := []Resource{
			testFileResource(t, "/workspace/data.csv"),
			testFileResource(t, "/workspace/data.csv"),
		}
		if err := ValidateResources(resources); err == nil {
			t.Fatal("ValidateResources() accepted duplicate paths")
		}
	})
	t.Run("allows paths that only overlap repositories outside uploads", func(t *testing.T) {
		resources := []Resource{
			{Record: db.SessionResource{ResourceType: "github_repository"}},
			testFileResource(t, "/workspace/repository/data.csv"),
		}
		if err := ValidateResources(resources); err != nil {
			t.Fatalf("ValidateResources(): %v", err)
		}
	})
	t.Run("accepts distinct paths", func(t *testing.T) {
		resources := []Resource{
			{Record: db.SessionResource{ResourceType: "github_repository"}},
			testFileResource(t, "/workspace/data.csv"),
			testFileResource(t, "/workspace/input/config.json"),
		}
		if err := ValidateResources(resources); err != nil {
			t.Fatalf("ValidateResources(): %v", err)
		}
	})
}

func TestPlanResourcesSharesFileBinding(t *testing.T) {
	resource := testFileResource(t, "/workspace/data.csv")
	resource.FileMIMEType = "text/csv"
	plan, err := PlanResources([]Resource{resource})
	if err != nil {
		t.Fatalf("PlanResources() error = %v", err)
	}
	if len(plan.Resources) != 1 || plan.Resources[0].FileMount == nil || len(plan.EventFileBindings) != 1 {
		t.Fatalf("PlanResources() = %+v", plan)
	}
	mount := plan.Resources[0].FileMount
	binding := plan.EventFileBindings[0]
	if binding.FileID != mount.FileExternalID || binding.Path != mount.Path || binding.MimeType != "text/csv" {
		t.Fatalf("event binding = %+v, file mount = %+v", binding, mount)
	}
}

func testFileResource(t *testing.T, mountPath string) Resource {
	t.Helper()
	raw, err := json.Marshal(mountPath)
	if err != nil {
		t.Fatalf("marshal mount path: %v", err)
	}
	spec, err := sessionresource.NormalizeFileSpec("file_test", "data.csv", nil, raw)
	if err != nil {
		t.Fatalf("normalize FileSpec: %v", err)
	}
	return Resource{
		Record: db.SessionResource{
			ExternalID:   "sesrsc_test",
			ResourceType: sessionresource.FileType,
		},
		FileSpec: new(spec),
	}
}
