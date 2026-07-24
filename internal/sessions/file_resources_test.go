package sessions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

func TestValidateSessionResourceMounts(t *testing.T) {
	t.Run("rejects too many files", func(t *testing.T) {
		resources := make([]db.SessionResource, 0, sandboxmount.MaxFileResources+1)
		for index := 0; index <= sandboxmount.MaxFileResources; index++ {
			resources = append(resources, testFileResource("/workspace/files/"+strings.Repeat("x", index+1)))
		}
		if err := validateSessionResourceMounts(resources); err == nil {
			t.Fatal("validateSessionResourceMounts() accepted more than 100 files")
		}
	})
	t.Run("rejects duplicate paths", func(t *testing.T) {
		resources := []db.SessionResource{
			testFileResource("/workspace/data.csv"),
			testFileResource("/workspace/data.csv"),
		}
		if err := validateSessionResourceMounts(resources); err == nil {
			t.Fatal("validateSessionResourceMounts() accepted duplicate paths")
		}
	})
	t.Run("allows paths that only overlap repositories outside uploads", func(t *testing.T) {
		resources := []db.SessionResource{
			testMountResource("github_repository", "/workspace/repository"),
			testFileResource("/workspace/repository/data.csv"),
		}
		if err := validateSessionResourceMounts(resources); err != nil {
			t.Fatalf("validateSessionResourceMounts(): %v", err)
		}
	})
	t.Run("accepts distinct paths", func(t *testing.T) {
		resources := []db.SessionResource{
			testMountResource("github_repository", "/workspace/repository"),
			testFileResource("/workspace/data.csv"),
			testFileResource("/workspace/input/config.json"),
		}
		if err := validateSessionResourceMounts(resources); err != nil {
			t.Fatalf("validateSessionResourceMounts(): %v", err)
		}
	})
}

func testFileResource(mountPath string) db.SessionResource {
	return testMountResource("file", mountPath)
}

func testMountResource(resourceType, mountPath string) db.SessionResource {
	payload := map[string]any{
		"id":         "sesrsc_test",
		"type":       resourceType,
		"mount_path": mountPath,
	}
	if resourceType == "file" {
		payload["file_id"] = "file_test"
		payload["source"] = sandboxmount.FileSource
	}
	raw, _ := json.Marshal(payload)
	return db.SessionResource{
		ExternalID:   "sesrsc_test",
		ResourceType: resourceType,
		Payload:      raw,
	}
}
